import { runFlow, setupAuth } from './lib/flow.js';

const audio = open('./sample.ogg', 'b');

export const options = {
  scenarios: {
    smoke: {
      executor: 'per-vu-iterations',

      vus: 1,
      iterations: 1,

      maxDuration: '1m',
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
  runFlow(data, audio, 'smoke', false);
}
