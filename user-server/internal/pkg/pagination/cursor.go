package pagination

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

const CursorPageSize = 100

// orderClauseRe 与 repository.generic 同源："列名 ASC/DESC" 逗号串的语法子集。
var orderClauseRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*( (asc|desc))(,[ ]*[a-z_][a-z0-9_]*( (asc|desc)))*$`)

// Cursor 编码后的游标（base64 + 时间戳 + ID）
type Cursor string

// EncodeCursor 将 (timestamp, id) 编码为 base64 游标
// 格式：<unix_nano>:<id>
//
//	例如：1700000000000000000:12345
func EncodeCursor(ts time.Time, id uint64) Cursor {
	raw := fmt.Sprintf("%d:%d", ts.UnixNano(), id)
	return Cursor(base64.URLEncoding.EncodeToString([]byte(raw)))
}

// DecodeCursor 解码游标
// 返回 (timestamp, id, ok)
func DecodeCursor(c Cursor) (time.Time, uint64, bool) {
	if c == "" {
		return time.Time{}, 0, false
	}
	raw, err := base64.URLEncoding.DecodeString(string(c))
	if err != nil {
		return time.Time{}, 0, false
	}
	var (
		tsNano int64
		id     uint64
	)
	parts := splitBytes(raw, ':')
	if len(parts) != 2 {
		return time.Time{}, 0, false
	}
	if _, err := fmt.Sscanf(string(parts[0]), "%d", &tsNano); err != nil {
		return time.Time{}, 0, false
	}
	if _, err := fmt.Sscanf(string(parts[1]), "%d", &id); err != nil {
		return time.Time{}, 0, false
	}
	return time.Unix(0, tsNano), id, true
}

func splitBytes(b []byte, sep byte) [][]byte {
	var result [][]byte
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == sep {
			result = append(result, b[start:i])
			start = i + 1
		}
	}
	result = append(result, b[start:])
	return result
}

// CursorQueryOpts cursor-based 查询参数
type CursorQueryOpts struct {
	// DB 原始表
	Table string
	// 游标（首次传空）
	Cursor Cursor
	// 单页大小
	PageSize int
	// 排序字段（默认 created_at DESC, id DESC）
	OrderBy string
	// 过滤条件（可选的 WHERE 子句）
	Where map[string]any
	// 结果集指针（如 &[]Model{}，GORM Find 要求指针）
	Dest any
}

// CursorQueryResult cursor-based 查询结果
type CursorQueryResult struct {
	Items      any
	NextCursor Cursor
	HasMore    bool
}

// CursorQuery 执行 cursor-based 分页查询
// 适用场景：created_at + id 唯一索引的大表
//
// 实现：
//  1. 首次查询：WHERE 过滤 + ORDER BY ts DESC, id DESC + LIMIT N+1
//  2. 取最后一条的 (ts, id) 作为 nextCursor
//  3. 后续查询：WHERE (ts, id) < (cursor.ts, cursor.id) + LIMIT N+1
//  4. 比 N 多取 1 条用于判断 hasMore
func CursorQuery(ctx context.Context, db *gorm.DB, opts CursorQueryOpts) (*CursorQueryResult, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > CursorPageSize {
		pageSize = CursorPageSize
	}

	q := db.WithContext(ctx).Table(opts.Table)

	for k, v := range opts.Where {
		q = q.Where(k, v)
	}

	orderBy := strings.ToLower(strings.TrimSpace(opts.OrderBy))
	if orderBy == "" {
		orderBy = "created_at DESC, id DESC"
	} else if !orderClauseRe.MatchString(orderBy) {
		// 结构白名单：CursorQuery 目前无生产调用方，但作为通用模板预拒
		// "列名 + ASC/DESC"语法子集之外的一切输入（fail-closed，绝不带病执行）。
		return nil, fmt.Errorf("invalid order_by: %s", opts.OrderBy)
	}
	q = q.Order(orderBy)

	if opts.Cursor != "" {
		ts, id, ok := DecodeCursor(opts.Cursor)
		if !ok {
			return nil, fmt.Errorf("invalid cursor: %s", opts.Cursor)
		}

		q = q.Where("(created_at, id) < (?, ?)", ts, id)
	}

	if err := q.Limit(pageSize + 1).Find(opts.Dest).Error; err != nil {
		return nil, err
	}

	count := sliceLen(opts.Dest)
	hasMore := count > pageSize
	if hasMore {

		sliceTruncate(opts.Dest, pageSize)
		count = pageSize
	}

	var nextCursor Cursor
	if hasMore && count > 0 {

		lastItem := sliceAt(opts.Dest, count-1)
		nextCursor = extractCursorFromItem(lastItem)
	}

	return &CursorQueryResult{
		Items:      opts.Dest,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// NextCursor 取分页结果最后一条的游标，供客户端翻页使用。
// 当结果条数不足 pageSize（即已到末页）或结果为空时返回空游标。
func NextCursor(rows any, pageSize int) Cursor {
	n := sliceLen(rows)
	if n == 0 || n < pageSize {
		return ""
	}
	return extractCursorFromItem(sliceAt(rows, n-1))
}

func IsValidLimit(limit int) bool {
	return limit > 0 && limit <= CursorPageSize
}

// ClampLimit 限制 limit 在 [1, CursorPageSize] 范围内
func ClampLimit(limit int) int {
	if limit <= 0 {
		return CursorPageSize
	}
	if limit > CursorPageSize {
		return CursorPageSize
	}
	return limit
}
