import { HttpInterceptorFn } from '@angular/common/http';

import { getAgentToken } from './auth-token';

// Attaches the stored agent token, if any. The server leaves /api/health open
// and challenges everything else when ADMIN_TOKEN is set.
export const authInterceptor: HttpInterceptorFn = (request, next) => {
  const token = getAgentToken();
  if (token && request.url.startsWith('/api')) {
    return next(request.clone({ setHeaders: { Authorization: `Bearer ${token}` } }));
  }
  return next(request);
};
