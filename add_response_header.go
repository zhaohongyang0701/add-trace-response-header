package add_trace_response_header

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
)

var (
	_ interface {
		http.ResponseWriter
		http.Hijacker
	} = &wrappedResponseWriter{}
)

type Config struct {
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	Regexp      string `json:"regexp,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	Overwrite   bool   `json:"overwrite,omitempty"`
}

func CreateConfig() *Config {
	return &Config{
		Regexp:      "^(.*)$",
		Replacement: "$1",
	}
}

type plugin struct {
	name   string
	next   http.Handler
	config *Config
	regex  *regexp.Regexp
}

type wrappedResponseWriter struct {
	w    http.ResponseWriter
	buf  *bytes.Buffer
	code int
}

func (w *wrappedResponseWriter) Header() http.Header {
	return w.w.Header()
}

func (w *wrappedResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *wrappedResponseWriter) WriteHeader(code int) {
	w.code = code
}

func (w *wrappedResponseWriter) Flush() {
	w.w.WriteHeader(w.code)
	io.Copy(w.w, w.buf)
}

func (w *wrappedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.w.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("%T is not an http.Hijacker", w.w)
	}

	return hijacker.Hijack()
}

// CustomContext 是一个实现了 context.Context 接口的自定义类型
type CustomContext struct {
	context.Context
	values map[interface{}]interface{}
}

// NewCustomContext 创建一个新的 CustomContext
func NewCustomContext(parent context.Context) *CustomContext {
	return &CustomContext{
		Context: parent,
		values:  make(map[interface{}]interface{}),
	}
}

// WithValue 用于设置键值对
func (c *CustomContext) WithValue(key, value interface{}) *CustomContext {
	c.values[key] = value
	return c
}

// Value 获取键对应的值
func (c *CustomContext) Value(key interface{}) interface{} {
	if val, exists := c.values[key]; exists {
		return val
	}
	return c.Context.Value(key)
}

// 打印所有键值对
func (c *CustomContext) PrintValues() {
	fmt.Println("Context values:")
	for key, val := range c.values {
		fmt.Printf("Key: %v, Value: %v\n", key, val)
	}
}

func (p *plugin) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 使用自定义的 CustomContext
	ctx := NewCustomContext(req.Context())

	// 打印所有 context 中的值
	ctx.PrintValues()

	// 在自定义 context 中设置一个新的值
	ctx = ctx.WithValue("newKey", "newValue")

	// 打印更新后的 context 值
	ctx.PrintValues()

	resp := &wrappedResponseWriter{
		w:    w,
		buf:  &bytes.Buffer{},
		code: 200,
	}
	defer resp.Flush()

	p.next.ServeHTTP(resp, req)

	if !p.config.Overwrite && resp.Header().Get(p.config.To) != "" {
		return
	}

	src := req.Header.Get(p.config.From)
	if src == "" {
		return
	}

	var replacement []byte
	for _, match := range p.regex.FindAllStringSubmatchIndex(src, -1) {
		replacement = p.regex.ExpandString(
			replacement,
			p.config.Replacement,
			src,
			match,
		)
	}
	if len(replacement) > 0 {
		if traceID, ok := req.Context().Value("tracing.traceID").(string); ok {
			resp.Header().Set(p.config.To, traceID)
		}
	}
}

func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.From == "" {
		return nil, fmt.Errorf("from cannot be empty")
	}
	if config.To == "" {
		return nil, fmt.Errorf("to cannot be empty")
	}

	regex, err := regexp.Compile(config.Regexp)
	if err != nil {
		return nil, fmt.Errorf("failed to compile regexp: %w", err)
	}

	return &plugin{
		name:   name,
		next:   next,
		config: config,
		regex:  regex,
	}, nil
}
