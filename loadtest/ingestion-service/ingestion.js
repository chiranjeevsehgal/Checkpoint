import { runFlow, setupAuth } from './lib/flow.js';

const audio = open('./sample.ogg', 'b');

export const options = {
  scenarios: {
    ingestion_users: {
      executor: 'ramping-vus',

      startVUs: 0,

      stages: [
        { duration: '1m', target: 50 },
        { duration: '2m', target: 200 },
        { duration: '2m', target: 500 },
        { duration: '2m', target: 1000 },

        // Hold 1000 concurrent users.
        { duration: '5m', target: 1000 },

        { duration: '1m', target: 0 },
      ],

      gracefulRampDown: '30s',
    },
  },

  thresholds: {
    // Overall HTTP error rate.
    http_req_failed: ['rate<0.01'],

    // Ingestion API endpoints.
    'http_req_duration{endpoint:create}': [
      'p(95)<500',
    ],

    'http_req_duration{endpoint:complete}': [
      'p(95)<500',
    ],

    'http_req_duration{endpoint:get}': [
      'p(95)<500',
    ],

    // MinIO upload will naturally take longer.
    'http_req_duration{endpoint:minio_upload}': [
      'p(95)<5000',
    ],
  },
};


export function setup() {
  return setupAuth();
}

export default function (data) {
  runFlow(data, audio, 'load', false);
}
