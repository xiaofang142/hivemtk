import { buildShopeeAdapter } from '../channels/shopee.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.SHOPEE, buildShopeeAdapter);
