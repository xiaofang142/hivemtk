import { buildPddAdapter } from '../channels/pdd.js';
import { startBridge } from './common.js';
import { CHANNELS } from '../core/types.js';

startBridge(CHANNELS.PDD, buildPddAdapter);
