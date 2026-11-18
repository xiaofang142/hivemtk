// 独立MAIN入口：只安装平台只读身份读器，不导入任何扩展业务能力。
import { installDouyinIdentityReader } from '../channels/douyin-identity.js';

installDouyinIdentityReader(document);
