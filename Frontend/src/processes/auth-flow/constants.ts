// §5.3: silent-refresh срабатывает за 60s до exp. Буфер — при 15-мин access-токене
// покрывает tab-resume, clock skew и latency /auth/refresh.
export const REFRESH_LEAD_MS = 60_000;
