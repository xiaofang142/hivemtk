package etl

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
	"golang.org/x/net/html"
)

// ExtractText 从原始文档字节中提取纯文本，按文件扩展名选择解析器。
// 这是「整本文档导入 → 切片 → 向量化 → 入库」链路的第一步：
// 必须先拿到可读文本，否则后续切片/向量/检索都是二进制乱码。
//
// 支持的格式：
//   - 纯文本类(.txt/.md/.csv/.json/.log/.xml/.yaml 等)：直接作为 UTF-8 文本返回
//   - HTML(.html/.htm)：剥离标签，保留段落/换行结构
//   - DOCX(.docx)：解压后解析 word/document.xml（标准库实现，无需外部依赖）
//   - PDF(.pdf)：使用 ledongthuc/pdf 逐页提取文本
//   - 其它/未知：退化为 UTF-8 文本（尽量不丢内容）
func ExtractText(filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".log", ".xml", ".yml", ".yaml", ".text":
		return string(data), nil
	case ".html", ".htm":
		return extractHTMLText(data), nil
	case ".docx":
		return extractDocx(data)
	case ".pdf":
		return extractPDF(data)
	case ".doc":
		return "", fmt.Errorf("不支持的旧版 .doc 二进制格式，请先转换为 .docx 或 .pdf 后再导入")
	default:
		return string(data), nil
	}
}

func extractHTMLText(data []byte) string {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return string(data)
	}
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				buf.WriteString(text)
				buf.WriteString(" ")
			}
		} else if n.Type == html.ElementNode {
			switch n.Data {
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				buf.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return collapseSpaces(buf.String())
}

// 第十六轮审计（2026-09-19）：外部文档是"小文件大输出"的放大攻击面——
// zip 的 deflate 最大膨胀比约 1000:1，一个贴着上传上限的 .docx 可让
// word/document.xml 解出数 GB 直接把进程打爆（内存 DoS）。所有解析器
// 的**解压/累积输出**统一在此设硬上限，超上限视为炸弹/畸形文档报错，
// 调用方（knowledge pipeline）报错后退化为原始字节（尺寸已被上传限住）。
const (
	// maxDocxEntryInflated 单个 zip 条目解压后的最大字节数
	maxDocxEntryInflated = 32 << 20 // 32MB
	// maxDocText 解析累积文本的最大字节数（docx body / pdf 全文共用）
	maxDocText = 64 << 20 // 64MB
)

func extractDocx(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("打开 docx(zip) 失败: %w", err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			// +1 读一字节越界探针：据此区分"恰好到限"与"超限炸弹"
			raw, err := io.ReadAll(io.LimitReader(rc, maxDocxEntryInflated+1))
			_ = rc.Close()
			if err != nil {
				return "", err
			}
			if int64(len(raw)) > maxDocxEntryInflated {
				return "", fmt.Errorf("word/document.xml 解压后超过 %dMB 上限（疑似 zip 炸弹文档）", maxDocxEntryInflated>>20)
			}
			return extractDocxBody(raw)
		}
	}
	return "", fmt.Errorf("docx 中未找到 word/document.xml")
}

func extractDocxBody(raw []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = false
	var sb strings.Builder
	inText := false
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "p":
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
			case "tab":
				sb.WriteString("\t")
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
		case xml.CharData:
			if inText {
				sb.WriteString(string(t))
			}
		}
	}
	return collapseSpaces(sb.String()), nil
}

func extractPDF(data []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("打开 pdf 失败: %w", err)
	}
	var sb strings.Builder
	pages := reader.NumPage()
	for i := 1; i <= pages; i++ {
		page := reader.Page(i)
		_ = i
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		if strings.TrimSpace(text) != "" {
			sb.WriteString(text)
			sb.WriteString("\n")
			// 畸形 PDF（如构造的天文数字页数/文本流）同样按炸弹口径熔断
			if sb.Len() > maxDocText {
				return "", fmt.Errorf("pdf 累积文本超过 %dMB 上限（疑似构造文档）", maxDocText>>20)
			}
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("pdf 未提取到任何文本内容（可能为扫描件/图片型 PDF）")
	}
	return collapseSpaces(sb.String()), nil
}

func collapseSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
