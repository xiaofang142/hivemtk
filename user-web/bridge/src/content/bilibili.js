import { buildBilibiliAdapter } from '../channels/bilibili.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.BILIBILI, buildBilibiliAdapter);
