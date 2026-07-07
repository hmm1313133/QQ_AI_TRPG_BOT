// 剧本文件解析器：支持 PDF、Word(.docx) 和纯文本文件。
//
// PDF 解析使用 github.com/ledongthuc/pdf 轻量库。
// Word 解析使用 github.com/zakahan/docx2md 库转换为 Markdown（保留标题结构），
// 降级方案为正则提取 w:t 标签文本。
// 纯文本文件直接读取。
package script

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/zakahan/docx2md"
)

// ParseFile 根据文件扩展名自动选择解析器，提取文本内容。
func ParseFile(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	log.Printf("[Parser] 解析文件: %s (格式: %s)", path, ext)
	switch ext {
	case ".pdf":
		return ParsePDF(path)
	case ".docx":
		return ParseDocx(path)
	case ".doc":
		return "", fmt.Errorf("旧版 .doc 格式不支持，请转换为 .docx 或 .pdf")
	case ".txt", ".md", ".text":
		return ParseText(path)
	default:
		log.Printf("[Parser] 未知扩展名 %s，尝试作为纯文本读取", ext)
		return ParseText(path)
	}
}

// ParsePDF 从 PDF 文件提取文本内容。
func ParsePDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开 PDF 失败: %w", err)
	}
	defer f.Close()

	totalPages := r.NumPage()
	log.Printf("[Parser] PDF 共 %d 页，开始提取文本", totalPages)

	var sb strings.Builder
	for i := 1; i <= totalPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			log.Printf("[Parser] PDF 第 %d 页为空，跳过", i)
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			log.Printf("[Parser] PDF 第 %d 页解析失败，跳过: %v", i, err)
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n\n")
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "", fmt.Errorf("PDF 文件未提取到任何文本（可能是扫描件）")
	}
	log.Printf("[Parser] PDF 解析完成，提取文本 %d 字符", len([]rune(result)))
	return result, nil
}

// ParseDocx 从 Word .docx 文件提取文本内容。
// 使用 docx2md 库转换为 Markdown 格式（保留标题结构，更有利于 AI 识别），
// 失败时降级为正则提取 w:t 标签文本。
func ParseDocx(path string) (string, error) {
	log.Printf("[Parser] 解析 docx (docx2md): %s", path)

	// docx2md 会将图片提取到 outputDir，这里用临时目录
	tmpDir, err := os.MkdirTemp("", "docx2md_*")
	if err != nil {
		log.Printf("[Parser] 创建临时目录失败，降级正则解析: %v", err)
		return parseDocxFallback(path)
	}
	defer os.RemoveAll(tmpDir)

	_, mdText, err := docx2md.DocxConvert(path, tmpDir)
	if err != nil {
		log.Printf("[Parser] docx2md 解析失败，降级正则解析: %v", err)
		return parseDocxFallback(path)
	}

	result := strings.TrimSpace(mdText)
	if result == "" {
		log.Printf("[Parser] docx2md 提取为空，降级正则解析")
		return parseDocxFallback(path)
	}

	log.Printf("[Parser] docx2md 解析完成，提取 %d 字符 (Markdown)", len([]rune(result)))
	return result, nil
}

// parseDocxFallback 降级方案：用正则从 document.xml 提取 w:t 标签文本。
func parseDocxFallback(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	xmlData, err := readDocxXMLWithZip(data)
	if err != nil {
		return "", err
	}
	return parseDocxXMLFromBytes(xmlData)
}

// parseDocxXMLFromBytes 解析 document.xml 提取文本（降级方案）。
func parseDocxXMLFromBytes(data []byte) (string, error) {
	xmlStr := string(data)

	// 按 </w:p> 分段提取 w:t 文本
	var result strings.Builder
	paragraphs := strings.Split(xmlStr, "</w:p>")
	tExtractRe := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)

	for _, para := range paragraphs {
		texts := tExtractRe.FindAllStringSubmatch(para, -1)
		if len(texts) > 0 {
			for _, t := range texts {
				if len(t) >= 2 {
					result.WriteString(t[1])
				}
			}
			result.WriteString("\n")
		}
	}

	out := strings.TrimSpace(result.String())
	if out == "" {
		return "", fmt.Errorf("docx 文件未提取到任何文本")
	}
	log.Printf("[Parser] docx 降级解析完成，提取 %d 字符", len([]rune(out)))
	return out, nil
}

// ParseText 读取纯文本文件。
func ParseText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	return string(data), nil
}

// ParseFromURL 从 HTTP(S) URL 下载文件并解析文本内容。
func ParseFromURL(url string) (string, error) {
	log.Printf("[Parser] 下载文件: %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("下载文件失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	log.Printf("[Parser] HTTP %d, Content-Type=%s, ContentLength=%d",
		resp.StatusCode, resp.Header.Get("Content-Type"), resp.ContentLength)

	data, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024)) // 限制 50MB
	if err != nil {
		return "", fmt.Errorf("读取下载内容失败: %w", err)
	}

	log.Printf("[Parser] 下载完成: %d 字节, 开始解析", len(data))
	return ParseFromBytes(data, url)
}

// ParseFromBytes 从内存字节解析文本内容，根据文件名后缀判断格式。
func ParseFromBytes(data []byte, filenameOrURL string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filenameOrURL))
	if ext == "" {
		return string(data), nil
	}

	switch ext {
	case ".pdf":
		return parsePDFFromBytes(data)
	case ".docx":
		return parseDocxFromBytes(data)
	case ".doc":
		return "", fmt.Errorf("旧版 .doc 格式不支持，请转换为 .docx 或 .pdf")
	case ".txt", ".md", ".text":
		return string(data), nil
	default:
		return string(data), nil
	}
}

// parsePDFFromBytes 从内存字节解析 PDF。
func parsePDFFromBytes(data []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "script_*.pdf")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}
	tmpFile.Close()

	return ParsePDF(tmpFile.Name())
}

// parseDocxFromBytes 从内存字节解析 .docx。
// 先写入临时文件再用 docx2md 解析，失败时降级正则方案。
func parseDocxFromBytes(data []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "script_*.docx")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}
	tmpFile.Close()

	return ParseDocx(tmpFile.Name())
}

// readDocxXMLWithZip 用 archive/zip 从 docx 字节读取 document.xml。
func readDocxXMLWithZip(data []byte) ([]byte, error) {
	r, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("打开 docx 数据失败: %w", err)
	}

	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("读取 document.xml 失败: %w", err)
			}
			defer rc.Close()
			xmlData, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("读取 document.xml 内容失败: %w", err)
			}
			return xmlData, nil
		}
	}
	return nil, fmt.Errorf("docx 数据中未找到 word/document.xml")
}
