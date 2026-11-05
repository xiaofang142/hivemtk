import { buildJdAdapter } from '../channels/jd.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.JD, buildJdAdapter);
