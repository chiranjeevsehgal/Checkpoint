import { runFlow, setupAuth } from './lib/flow.js';

const audio = open('./sample.ogg', 'b');

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


export function setup() {
  return setupAuth();
}

export default function (data) {
  runFlow(data, audio, 'spike', false);
}
