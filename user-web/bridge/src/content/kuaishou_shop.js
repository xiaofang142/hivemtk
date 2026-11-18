import { buildKuaishouShopAdapter } from '../channels/kuaishou_shop.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.KUAISHOU_SHOP, buildKuaishouShopAdapter);
