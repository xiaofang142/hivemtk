import { buildTaobaoAdapter } from '../channels/taobao.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.TAOBAO, buildTaobaoAdapter);
