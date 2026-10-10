import { http } from '@/utils/request'

export const getExperimentWithStats = (id, method = 'frequentist') =>
  http.get(`/api/ab-experiments/${id}/stats`, { method });

export const getExperimentDiagnostics = (id) =>
  http.get(`/api/ab-experiments/${id}/diagnostics`)

export const getExperimentWithCUPED = (id) =>
  http.get(`/api/ab-experiments/${id}/cuped`);

export const sequentialTest = (id, alpha = 0.05) =>
  http.post(`/api/ab-experiments/${id}/sequential-test`, { alpha });

export const bayesianTest = (id) =>
  http.post(`/api/ab-experiments/${id}/bayesian-test`, {});
