import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';

// Shared upload flow for every ingestion-service scenario: create the upload,
// PUT bytes to the presigned MinIO URL, complete it, then read it back.
// Scenario files keep only their options; this module owns the flow.

export function baseUrl() {
  return __ENV.BASE_URL || 'http://localhost:8080';
}

function abortMissing() {
  exec.test.abort(
    'Provide SESSION_TOKEN + DEVICE_ID (static Kratos session), or KRATOS_URL + KRATOS_EMAIL + KRATOS_PASSWORD + DEVICE_ID for auto-refresh.',
  );
}

// Password login against the Kratos public API: GET a login flow, submit it,
// return the session token. Mirrors checkpoint-app's kratos-client.
export function loginWithPassword(kratosUrl, email, password) {
  const base = (kratosUrl || 'http://localhost:4433').replace(/\/+$/, '');
  const flowResponse = http.get(`${base}/self-service/login/api`, {
    tags: { name: 'kratos_login_flow', endpoint: 'kratos' },
  });
  if (flowResponse.status !== 200) {
    console.error(`Kratos login flow failed: ${flowResponse.status} ${flowResponse.body}`);
    return null;
  }
  let action;
  try {
    action = flowResponse.json().ui.action;
  } catch (e) {
    console.error(`Invalid Kratos flow response: ${flowResponse.body}`);
    return null;
  }
  const submitResponse = http.post(
    action,
    JSON.stringify({ method: 'password', identifier: email, password }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'kratos_login', endpoint: 'kratos' } },
  );
  if (submitResponse.status !== 200) {
    console.error(`Kratos login failed: ${submitResponse.status} ${submitResponse.body}`);
    return null;
  }
  let token;
  try {
    token = submitResponse.json().session_token;
  } catch (e) {
    token = null;
  }
  if (!token) console.error('Kratos login returned no session_token');
  return token || null;
}

// k6 setup(): refresh the session when password credentials are configured
// (long runs outlive a static token), otherwise fall back to SESSION_TOKEN.
export function setupAuth() {
  const deviceId = __ENV.DEVICE_ID;
  if (__ENV.KRATOS_EMAIL && __ENV.KRATOS_PASSWORD) {
    const token = loginWithPassword(__ENV.KRATOS_URL, __ENV.KRATOS_EMAIL, __ENV.KRATOS_PASSWORD);
    if (token && deviceId) return { sessionToken: token, deviceId };
    console.warn('Password login failed; falling back to SESSION_TOKEN.');
  }
  if (!__ENV.SESSION_TOKEN || !deviceId) abortMissing();
  return { sessionToken: __ENV.SESSION_TOKEN, deviceId };
}

function authOf(data) {
  if (data && data.sessionToken && data.deviceId) return data;
  if (!__ENV.SESSION_TOKEN || !__ENV.DEVICE_ID) abortMissing();
  return { sessionToken: __ENV.SESSION_TOKEN, deviceId: __ENV.DEVICE_ID };
}

export function runFlow(data, audio, keyPrefix, uniqueKey) {
  const { sessionToken, deviceId } = authOf(data);
  const base = baseUrl();
  const audioSize = audio.byteLength;

  //
  // 1. CREATE UPLOAD
  //

  const idempotencyKey = uniqueKey
    ? `${keyPrefix}-${exec.vu.idInTest}-${exec.vu.iterationInScenario}-${Date.now()}`
    : `${keyPrefix}-${exec.vu.idInTest}-${exec.vu.iterationInScenario}`;

  const createResponse = http.post(
    `${base}/v1/uploads`,
    JSON.stringify({
      filename: 'load-test.ogg',
      content_type: 'audio/ogg',
      device_id: deviceId,
      size_bytes: audioSize,
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${sessionToken}`,
        'Idempotency-Key': idempotencyKey,
      },
      tags: { name: 'create', endpoint: 'create' },
    },
  );

  const created = check(createResponse, {
    'create status = 201': (r) => r.status === 201,
  });
  if (!created) {
    console.error(`CREATE FAILED: ${createResponse.status} ${createResponse.body}`);
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

  const putResponse = http.put(uploadUrl, audio, {
    headers: { 'Content-Type': 'audio/ogg' },
    tags: { name: 'minio_upload', endpoint: 'minio_upload' },
  });
  const uploaded = check(putResponse, {
    'minio upload successful': (r) => r.status >= 200 && r.status < 300,
  });
  if (!uploaded) {
    console.error(`MINIO FAILED: ${putResponse.status}`);
    return;
  }

  //
  // 3. COMPLETE UPLOAD
  //

  const completeResponse = http.post(
    `${base}/v1/uploads/${uploadId}/complete`,
    JSON.stringify({ size_bytes: audioSize }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${sessionToken}`,
      },
      tags: { name: 'complete', endpoint: 'complete' },
    },
  );
  const completed = check(completeResponse, {
    'complete status = 200': (r) => r.status === 200,
    'complete status READY or SUBMITTED': (r) => {
      if (r.status !== 200) return false;
      const body = r.json();
      return body.status === 'READY' || body.status === 'SUBMITTED';
    },
  });
  if (!completed) {
    console.error(`COMPLETE FAILED: ${completeResponse.status} ${completeResponse.body}`);
    return;
  }

  //
  // 4. GET UPLOAD
  //

  const getResponse = http.get(`${base}/v1/uploads/${uploadId}`, {
    headers: { Authorization: `Bearer ${sessionToken}` },
    tags: { name: 'get', endpoint: 'get' },
  });
  check(getResponse, {
    'get status = 200': (r) => r.status === 200,
  });

  //
  // Realistic "user think time"
  //

  sleep(1);
}
