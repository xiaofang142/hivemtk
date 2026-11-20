// 共享 HTTP 契约使用 Node 内置测试工具，包目录无需安装 Vitest。
import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  BIZ_CODE_MODULE_NOT_ENABLED,
  BIZ_CODE_SUBSCRIPTION_EXPIRED,
  isBizSuccess,
  isJsonContentType,
  extractServerMessage,
  createBizError
} from './index.js'

describe('isBizSuccess', () => {
  it('识别三种成功拼写', () => {
    assert.equal(isBizSuccess('SUCCESS'), true)
    assert.equal(isBizSuccess(200), true)
    assert.equal(isBizSuccess(0), true)
  })
  it('非成功码不误判', () => {
    assert.equal(isBizSuccess(4301), false)
    assert.equal(isBizSuccess(4305), false)
    assert.equal(isBizSuccess(undefined), false)
    assert.equal(isBizSuccess('success'), false)
  })
})

describe('isJsonContentType', () => {
  it('application/json 命中（大小写不敏感）', () => {
    assert.equal(isJsonContentType({ 'content-type': 'application/json; charset=utf-8' }), true)
    assert.equal(isJsonContentType({ 'Content-Type': 'Application/JSON' }), true)
  })
  it('非 JSON 与空 headers 不命中', () => {
    assert.equal(isJsonContentType({ 'content-type': 'text/html' }), false)
    assert.equal(isJsonContentType(null), false)
    assert.equal(isJsonContentType({}), false)
  })
})

describe('extractServerMessage', () => {
  it('message 优先于 msg，缺失回落 fallback', () => {
    assert.equal(extractServerMessage({ message: 'a', msg: 'b' }, 'fb'), 'a')
    assert.equal(extractServerMessage({ msg: 'b' }, 'fb'), 'b')
    assert.equal(extractServerMessage({}, 'fb'), 'fb')
    assert.equal(extractServerMessage(null, 'fb'), 'fb')
    assert.equal(extractServerMessage('string body', 'fb'), 'fb')
  })
})

describe('createBizError', () => {
  it('挂 status / bizCode 机器读字段', () => {
    const e = createBizError('msg', { status: 403 }, 4301)
    assert.equal(e.message, 'msg')
    assert.equal(e.status, 403)
    assert.equal(e.bizCode, 4301)
  })
  it('缺省 response ⇒ status 0；未传 bizCode 不挂字段', () => {
    const e = createBizError('m', null)
    assert.equal(e.status, 0)
    assert.equal('bizCode' in e, false)
  })
  it('markReported 钩子生效（应用侧上报注入）', () => {
    const e = createBizError('m', null, 1, (x) => ({ ...x, reported: true }))
    assert.equal(e.reported, true)
  })
})

describe('业务码常量', () => {
  it('4301/4305 与 user-web 旧值一致（迁移契约）', () => {
    assert.equal(BIZ_CODE_MODULE_NOT_ENABLED, 4301)
    assert.equal(BIZ_CODE_SUBSCRIPTION_EXPIRED, 4305)
  })
})
