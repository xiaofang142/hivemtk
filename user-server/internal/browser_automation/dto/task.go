package dto

// StepItem 编排步骤（前端 → 后端落库 → executor 解释执行）
type StepItem struct {
	Action        string            `json:"action" binding:"required,oneof=open_tab click type click_near post_comment snapshot markdown screenshot wait wait_for_selector scroll extract assert query close_tab"`
	Target        string            `json:"target"`                                                 // click/type/extract 用：selector 或 @e{N} refs
	Value         string            `json:"value"`                                                  // type 用
	Ms            int               `json:"ms"`                                                     // wait 用
	ClearFirst    bool              `json:"clear_first"`                                            // type 用
	SubmitOnEnter bool              `json:"submit_on_enter"`                                        // type 用
	Direction     string            `json:"direction" binding:"omitempty,oneof=up down left right"` // scroll 用
	Amount        int               `json:"amount"`                                                 // scroll 用
	Selector      string            `json:"selector"`                                               // wait_for_selector 用
	TimeoutMs     int               `json:"timeout_ms"`                                             // wait_for_selector 用
	Selectors     map[string]string `json:"selectors"`                                              // extract 用：key→CSS selector 多键提取
	// click_near 用：以 Anchor（CSS selector）为基准，点击其容器内文本含 ButtonText 的 button
	Anchor     string `json:"anchor"`
	ButtonText string `json:"button_text"`
	// assert/query 用（洞察层原语）
	AssertKind string `json:"assert_kind" binding:"omitempty,oneof=contains_text selector_exists"` // assert 子类型
	QueryKind  string `json:"query_kind" binding:"omitempty,oneof=text exists count attr"`         // query 子类型
	// 错误处理策略
	ContinueOnError bool `json:"continue_on_error"` // 默认 false；true 则此步失败后继续下一步
	RetryCount      int  `json:"retry_count" binding:"omitempty,min=0,max=10"`
	RetryBackoffMs  int  `json:"retry_backoff_ms" binding:"omitempty,min=100"`
}

type CreateBrowserTaskReq struct {
	Name        string     `json:"name" binding:"required,max=256"`
	Description string     `json:"description"`
	TaskType    string     `json:"task_type" binding:"required,oneof=one_shot loop cron workflow"`
	Url         string     `json:"url" binding:"required,max=2048"`
	Platform    string     `json:"platform" binding:"omitempty,max=32"` // 平台标识（xiaohongshu/douyin/xianyu），空=xiaohongshu 兼容存量
	BrainMode   bool       `json:"brain_mode"`
	BrainGoal   string     `json:"brain_goal"`
	Steps       []StepItem `json:"steps"`
	LoopCount   int        `json:"loop_count" binding:"omitempty,min=1,max=1000"`
	DelayMs     int        `json:"delay_ms" binding:"omitempty,min=0,max=60000"`
	TimeoutSec  int        `json:"timeout_sec" binding:"omitempty,min=10,max=3600"`
	// workflow 依赖
	DependsOnTaskID *uint  `json:"depends_on_task_id"`
	DependsOnMode   string `json:"depends_on_mode" binding:"omitempty,oneof=all_done any_success"`
	// 失败自动重试
	RetryOnFail   bool `json:"retry_on_fail"`
	RetryDelaySec int  `json:"retry_delay_sec" binding:"omitempty,min=30,max=86400"`
	MaxRetryTimes int  `json:"max_retry_times" binding:"omitempty,min=0,max=10"`
}

type UpdateBrowserTaskReq struct {
	Name        string     `json:"name" binding:"omitempty,max=256"`
	Description *string    `json:"description"`
	TaskType    string     `json:"task_type" binding:"omitempty,oneof=one_shot loop cron workflow"`
	Url         string     `json:"url" binding:"omitempty,max=2048"`
	Platform    *string    `json:"platform" binding:"omitempty,max=32"`
	BrainMode   *bool      `json:"brain_mode"`
	BrainGoal   *string    `json:"brain_goal"`
	Steps       []StepItem `json:"steps"`
	LoopCount   *int       `json:"loop_count" binding:"omitempty,min=1,max=1000"`
	DelayMs     *int       `json:"delay_ms" binding:"omitempty,min=0,max=60000"`
	TimeoutSec  *int       `json:"timeout_sec" binding:"omitempty,min=10,max=3600"`
}

type RunBrowserTaskReq struct {
	// 预留：后续支持带参运行（如注入变量），MVP 无必填参数
}

type ListTaskReq struct {
	Status   string `form:"status" binding:"omitempty,oneof=draft ready running paused done failed archived"`
	TaskType string `form:"task_type" binding:"omitempty,oneof=one_shot loop cron workflow"`
	Page     int    `form:"page"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=200"`
}
