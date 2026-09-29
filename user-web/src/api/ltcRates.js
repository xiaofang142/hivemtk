import { http } from '@/utils/request';

const LtcRatesApi = {
  getRates() {
    return http.get('/api/ltc-rates')
  }
};

export default LtcRatesApi
