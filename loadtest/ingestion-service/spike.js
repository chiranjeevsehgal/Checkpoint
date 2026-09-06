import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

const audio = open('./sample.ogg', 'b');
const audioSize = audio.byteLength;

export const options = {
  scenarios: {
    spike: {
      executor: 'per-vu-iterations',

      vus: 1000,
      iterations: 1,

      maxDuration: '2m',
    },
  },

  thresholds: {
    http_req_failed: ['rate<0.01'],
  },
};


// Generate a different valid UUID for each VU.
function userId() {
  const id = exec.vu.idInTest;
  const suffix = id.toString(16).padStart(12, '0');

  return `aaaaaaaa-aaaa-4aaa-8aaa-${suffix}`;
}


export default function () {
  const user = userId();

  //
  // 1. CREATE UPLOAD
  //

  const createPayload = JSON.stringify({
    filename: 'load-test.ogg',
    content_type: 'audio/ogg',
    size_bytes: audioSize,
  });

  const createResponse = http.post(
    `${BASE_URL}/v1/uploads`,
    createPayload,
    {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${user}`,

        // Unique per logical request.
        'Idempotency-Key':
          `spike-${exec.vu.idInTest}-${exec.vu.iterationInScenario}`,
      },

      tags: {
        name: 'create',
        endpoint: 'create',
      },
    }
  );

  const created = check(createResponse, {
    'create status = 201': (r) => r.status === 201,
  });

  if (!created) {
    console.error(
      `CREATE FAILED: ${createResponse.status} ${createResponse.body}`
    );

    return;
  }


  let createBody;

  try {
    createBody = createResponse.json();
  } catch (e) {
    console.error(`Invalid create response: ${createResponse.body}`);
    return;
  }

  const uploadId = createBody.upload_id;
  const uploadUrl = createBody.upload.url;


  //
  // 2. DIRECT UPLOAD TO MINIO
  //

  const putResponse = http.put(
    uploadUrl,
    audio,
    {
      headers: {
        'Content-Type': 'audio/ogg',
      },

      tags: {
        name: 'minio_upload',
        endpoint: 'minio_upload',
      },
    }
  );

  const uploaded = check(putResponse, {
    'minio upload successful': (r) =>
      r.status >= 200 && r.status < 300,
  });

  if (!uploaded) {
    console.error(
      `MINIO FAILED: ${putResponse.status}`
    );

    return;
  }


  //
  // 3. COMPLETE UPLOAD
  //

  const completeResponse = http.post(
    `${BASE_URL}/v1/uploads/${uploadId}/complete`,
    JSON.stringify({
      size_bytes: audioSize,
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${user}`,
      },

      tags: {
        name: 'complete',
        endpoint: 'complete',
      },
    }
  );


  const completed = check(completeResponse, {
    'complete status = 200': (r) => r.status === 200,

    'complete status READY or SUBMITTED': (r) => {
      if (r.status !== 200) {
        return false;
      }

      const body = r.json();

      return (
        body.status === 'READY' ||
        body.status === 'SUBMITTED'
      );
    },
  });

  if (!completed) {
    console.error(
      `COMPLETE FAILED: ${completeResponse.status} ${completeResponse.body}`
    );

    return;
  }


  //
  // 4. GET UPLOAD
  //

  const getResponse = http.get(
    `${BASE_URL}/v1/uploads/${uploadId}`,
    {
      headers: {
        'Authorization': `Bearer ${user}`,
      },

      tags: {
        name: 'get',
        endpoint: 'get',
      },
    }
  );


  check(getResponse, {
    'get status = 200': (r) => r.status === 200,
  });


  //
  // Realistic "user think time"
  //

  sleep(1);
}
