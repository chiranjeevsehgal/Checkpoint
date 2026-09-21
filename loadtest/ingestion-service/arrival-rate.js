import { runFlow, setupAuth } from './lib/flow.js';

const audio = open('./sample.ogg', 'b');

export const options = {
  scenarios: {
    ingestion_rate: {
      executor: 'ramping-arrival-rate',

      startRate: 10,
      timeUnit: '1s',

      preAllocatedVUs: 100,
      maxVUs: 2000,

      stages: [
        { duration: '2m', target: 20 },
        { duration: '2m', target: 50 },
        { duration: '2m', target: 100 },
        { duration: '2m', target: 200 },
        { duration: '2m', target: 300 },
      ],
    },
  },

  thresholds: {
    http_req_failed: ['rate<0.01'],

    'http_req_duration{endpoint:create}': [
      'p(95)<500',
    ],

    'http_req_duration{endpoint:complete}': [
      'p(95)<500',
    ],

    'http_req_duration{endpoint:get}': [
      'p(95)<500',
    ],

    'http_req_duration{endpoint:minio_upload}': [
      'p(95)<5000',
    ],
  },
};


export function setup() {
  return setupAuth();
}

export default function (data) {
  runFlow(data, audio, 'rate', true);
}
