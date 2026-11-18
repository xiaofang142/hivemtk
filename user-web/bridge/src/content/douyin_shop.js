import { buildDouyinShopAdapter } from '../channels/douyin_shop.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.DOUYIN_SHOP, buildDouyinShopAdapter);
