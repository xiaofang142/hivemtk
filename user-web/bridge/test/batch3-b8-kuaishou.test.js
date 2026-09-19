import { describe, it, expect, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { getAccountId } from '../src/channels/kuaishou.js';
import { isExperimentalChannel, EXPERIMENTAL_CHANNELS } from '../src/core/constants.js';

// B8（批3）：kuaishou 能力如实降级 + getAccountId 路径末段误判修复。

describe('B8 getAccountId 只信 /profile/<id> 形态', () => {
  beforeEach(() => {
    localStorage.clear();
    history.replaceState(null, '', '/new-reco');
  });

  it('私信默认路径 /new-reco 不再被当成账号 id（旧正则 ≥6 字符必误判）', () => {
    expect(getAccountId()).toBe('');
    expect(localStorage.getItem('hivebridge:account:kuaishou')).toBe(null);
  });

  it('/profile/<userId> 提取并缓存', () => {
    history.replaceState(null, '', '/profile/3xyABC123456');
    expect(getAccountId()).toBe('3xyABC123456');
    expect(localStorage.getItem('hivebridge:account:kuaishou')).toBe('3xyABC123456');
  });

  it('非 profile 路径回落缓存值（跨页保持身份）', () => {
    localStorage.setItem('hivebridge:account:kuaishou', 'cached-id-1');
    history.replaceState(null, '', '/message/list');
    expect(getAccountId()).toBe('cached-id-1');
  });

  it('/profile/ 带 query 也能取到纯 id', () => {
    history.replaceState(null, '', '/profile/3zzTopLevel?k=1');
    expect(getAccountId()).toBe('3zzTopLevel');
  });
});

describe('B8 实验渠道声明', () => {
  it('kuaishou 是唯一实验渠道；行为面不拦截其余', () => {
    expect(isExperimentalChannel('kuaishou')).toBe(true);
    expect(isExperimentalChannel('douyin')).toBe(false);
    expect(isExperimentalChannel(undefined)).toBe(false);
    expect(Object.keys(EXPERIMENTAL_CHANNELS)).toEqual(['kuaishou']);
  });

  it('content 启动路径引用实验告警（源码契约）', () => {
    const src = readFileSync(join(process.cwd(), 'src', 'content', 'common.js'), 'utf8');
    expect(src).toContain('isExperimentalChannel');
    expect(src).toContain('实验渠道');
  });
});
