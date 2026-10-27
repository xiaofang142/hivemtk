// Package reach 跨域共享的触达哨兵值。
//
// 这些哨兵原先定义在 internal/service/proactive_reach.go，被同包多个域（短信/采集/
// 恢复队列/触达管道）共同使用。它们表达的是**业务语义**（收件人全局退订），
// 不属于某一个 service 子域——放在 pkg 层后，按域拆出的子包（如 service/sms）
// 可以直接依赖它，而不必反向 import 根 service 包形成环。
package reach

import "errors"

// ErrDoNotContact 命中全局退订/勿扰标志位：该收件人不可再外发。
var ErrDoNotContact = errors.New("do-not-contact")
