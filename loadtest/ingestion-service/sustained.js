import { runFlow, setupAuth } from './lib/flow.js';

const audio = open('./sample.ogg', 'b');

export const options = {
  scenarios: {
    sustained: {
      executor: 'constant-vus',

      vus: 1000,
      duration: '10m',
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
  runFlow(data, audio, 'sustained', false);
}
