import { buildWalmartUsAdapter } from '../channels/walmart_us.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.WALMART_US, buildWalmartUsAdapter);
