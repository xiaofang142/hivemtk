import { buildWeiboAdapter } from '../channels/weibo.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.WEIBO, buildWeiboAdapter);
