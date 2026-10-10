package logging

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// The console encoder is the operator-facing half of the "human vs machine"
// split described in the package docs: structured records stay complete in the
// entry itself, but a console line is laid out the way a person scans it —
// aligned time/level/component columns, a plain-language message, and bare
// `key=value` pairs instead of a trailing JSON object.
//
// Format:
//
//	2026-09-14 17:16:25.061 INFO  gb28181.register      GB28181 注册鉴权通过  device_id=35020000001310000999 call_id=... event=gb28181.register.succeeded
//
// Deliberate choices:
//   - No ANSI colour. Console output is also the file sink; escape codes would
//     corrupt the on-disk record and break `grep`.
//   - One record per line. Values containing control characters are escaped, so
//     a newline inside a field can never split a record.
//   - Nothing is dropped for being "boring" except the per-process constants
//     (see consoleConstants), and those are only dropped when the value matches
//     the value the runtime bound, so a call site logging its own `version`
//     still shows up.
const (
	consoleLevelWidth     = 5
	consoleComponentWidth = 20
)

// zap keeps its buffer pool internal, so the encoder owns one. Buffers are
// reused exactly like the built-in encoders: EncodeEntry returns a pooled
// buffer and the calling core frees it.
var consoleBuffers = buffer.NewPool()

// consoleFieldOrder ranks the per-record identifiers and verdict fields so that
// the same kind of value always lands in the same left-to-right position. Bare
// key=value pairs cannot be aligned into columns like a table, so a stable
// order is what lets a reader skip straight to the field they want. Unranked
// fields keep their call-site order and follow the ranked ones.
//
// This is presentation only: the json encoder and the log entry are untouched,
// and `grep key=` behaves identically either way.
var consoleFieldOrder = []string{
	"device_id", "channel_id", "platform_id", "node_id", "stream_id",
	"call_id", "request_id", "client_request_id", "execution_id", "operation_id", "correlation_id",
	"sn", "event", "stage", "outcome", "reason_code", "error", "error_class",
	"duration_ms", "transport",
}

var consoleFieldRank = func() map[string]int {
	ranks := make(map[string]int, len(consoleFieldOrder))
	for i, key := range consoleFieldOrder {
		ranks[key] = i
	}
	return ranks
}()

type consoleField struct{ key, value string }

// orderFields is stable: equally ranked and unranked fields keep call-site order.
func (e *consoleEncoder) orderFields() {
	ranked := false
	for _, f := range e.fields {
		if _, ok := consoleFieldRank[f.key]; ok {
			ranked = true
			break
		}
	}
	if !ranked {
		return
	}
	sort.SliceStable(e.fields, func(i, j int) bool {
		left, leftRanked := consoleFieldRank[e.fields[i].key]
		right, rightRanked := consoleFieldRank[e.fields[j].key]
		switch {
		case leftRanked && rightRanked:
			return left < right
		case leftRanked != rightRanked:
			return leftRanked
		default:
			return false
		}
	})
}

type consoleEncoder struct {
	cfg    zapcore.EncoderConfig
	prefix string
	fields []consoleField
	// constants maps a fully qualified key to the process-constant value bound
	// by the runtime. A field is omitted only when it matches that value.
	constants map[string]string
}

// consoleConstants builds the omission set for the values the runtime binds
// once per process. Repeating them on every line costs readability without
// adding information; the json encoder keeps them for machine consumers.
func consoleConstants(service, version, instance string) map[string]string {
	return map[string]string{"service": service, "version": version, "instance": instance}
}

func newConsoleEncoder(cfg zapcore.EncoderConfig, constants map[string]string) zapcore.Encoder {
	return &consoleEncoder{cfg: cfg, constants: constants}
}

func (e *consoleEncoder) Clone() zapcore.Encoder {
	clone := &consoleEncoder{cfg: e.cfg, prefix: e.prefix, constants: e.constants}
	if len(e.fields) > 0 {
		clone.fields = append([]consoleField(nil), e.fields...)
	}
	return clone
}

func (e *consoleEncoder) qualify(key string) string {
	if e.prefix == "" {
		return key
	}
	return e.prefix + "." + key
}

func (e *consoleEncoder) add(key, rendered string) {
	e.fields = append(e.fields, consoleField{key: e.qualify(key), value: rendered})
}

func (e *consoleEncoder) addString(key, raw string) {
	qualified := e.qualify(key)
	if value, ok := e.constants[qualified]; ok && value == raw {
		return
	}
	e.fields = append(e.fields, consoleField{key: qualified, value: consoleQuote(raw)})
}

// ObjectEncoder.

func (e *consoleEncoder) AddString(key, value string) { e.addString(key, value) }
func (e *consoleEncoder) AddBool(key string, value bool) {
	e.add(key, strconv.FormatBool(value))
}
func (e *consoleEncoder) AddInt(key string, value int) { e.add(key, strconv.Itoa(value)) }
func (e *consoleEncoder) AddInt8(key string, value int8) {
	e.add(key, strconv.FormatInt(int64(value), 10))
}
func (e *consoleEncoder) AddInt16(key string, value int16) {
	e.add(key, strconv.FormatInt(int64(value), 10))
}
func (e *consoleEncoder) AddInt32(key string, value int32) {
	e.add(key, strconv.FormatInt(int64(value), 10))
}
func (e *consoleEncoder) AddInt64(key string, value int64) {
	e.add(key, strconv.FormatInt(value, 10))
}
func (e *consoleEncoder) AddUint(key string, value uint) {
	e.add(key, strconv.FormatUint(uint64(value), 10))
}
func (e *consoleEncoder) AddUint8(key string, value uint8) {
	e.add(key, strconv.FormatUint(uint64(value), 10))
}
func (e *consoleEncoder) AddUint16(key string, value uint16) {
	e.add(key, strconv.FormatUint(uint64(value), 10))
}
func (e *consoleEncoder) AddUint32(key string, value uint32) {
	e.add(key, strconv.FormatUint(uint64(value), 10))
}
func (e *consoleEncoder) AddUint64(key string, value uint64) {
	e.add(key, strconv.FormatUint(value, 10))
}
func (e *consoleEncoder) AddUintptr(key string, value uintptr) {
	e.add(key, strconv.FormatUint(uint64(value), 10))
}
func (e *consoleEncoder) AddFloat32(key string, value float32) {
	e.add(key, strconv.FormatFloat(float64(value), 'g', -1, 32))
}
func (e *consoleEncoder) AddFloat64(key string, value float64) {
	e.add(key, strconv.FormatFloat(value, 'g', -1, 64))
}
func (e *consoleEncoder) AddComplex64(key string, value complex64) {
	e.add(key, strconv.FormatComplex(complex128(value), 'g', -1, 64))
}
func (e *consoleEncoder) AddComplex128(key string, value complex128) {
	e.add(key, strconv.FormatComplex(value, 'g', -1, 128))
}
func (e *consoleEncoder) AddDuration(key string, value time.Duration) {
	e.add(key, encodeDurationWith(e.cfg, value))
}
func (e *consoleEncoder) AddTime(key string, value time.Time) {
	e.add(key, encodeTimeWith(e.cfg, value))
}
func (e *consoleEncoder) AddBinary(key string, value []byte) {
	e.add(key, base64.StdEncoding.EncodeToString(value))
}
func (e *consoleEncoder) AddByteString(key string, value []byte) { e.addString(key, string(value)) }
func (e *consoleEncoder) AddReflected(key string, value interface{}) error {
	// Reached only when a call site passes an arbitrary value straight to the
	// inner core; the runtime core collapses reflected fields to a type name
	// before they get here. Keep it printable rather than panicking.
	e.add(key, consoleQuote(fmt.Sprintf("%v", value)))
	return nil
}
func (e *consoleEncoder) OpenNamespace(key string) { e.prefix = e.qualify(key) }
func (e *consoleEncoder) AddObject(key string, marshaler zapcore.ObjectMarshaler) error {
	sub := &consoleEncoder{cfg: e.cfg, constants: e.constants}
	if err := marshaler.MarshalLogObject(sub); err != nil {
		return err
	}
	return e.render(key, renderConsoleObject(sub.fields))
}
func (e *consoleEncoder) AddArray(key string, marshaler zapcore.ArrayMarshaler) error {
	items := &consoleArrayEncoder{cfg: e.cfg}
	if err := marshaler.MarshalLogArray(items); err != nil {
		return err
	}
	return e.render(key, "["+strings.Join(items.items, " ")+"]")
}

// render records a pre-rendered composite value. Composite values are never
// process constants, so the omission check does not apply.
func (e *consoleEncoder) render(key, rendered string) error {
	e.add(key, rendered)
	return nil
}

func (e *consoleEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	final := e.Clone().(*consoleEncoder)
	for _, f := range fields {
		if f.Type == zapcore.SkipType {
			continue
		}
		f.AddTo(final)
	}
	buf := consoleBuffers.Get()
	final.orderFields()
	// Separators are emitted only when a preceding segment exists, so the line
	// never carries trailing blanks. `head` tracks whether anything was written.
	head := false
	if e.cfg.EncodeTime != nil && !ent.Time.IsZero() {
		buf.AppendString(encodeTimeWith(e.cfg, ent.Time))
		head = true
	}
	if e.cfg.EncodeLevel != nil {
		if head {
			buf.AppendByte(' ')
		}
		buf.AppendString(consolePadLevel(encodeLevelWith(e.cfg, ent.Level)))
		head = true
	}
	if ent.LoggerName != "" {
		if head {
			buf.AppendByte(' ')
		}
		buf.AppendString(consolePadComponent(ent.LoggerName))
		head = true
	}
	if ent.Caller.Defined && e.cfg.CallerKey != "" && e.cfg.EncodeCaller != nil {
		if head {
			buf.AppendByte(' ')
		}
		buf.AppendString(encodeCallerWith(e.cfg, ent.Caller))
		head = true
	}
	if ent.Message != "" {
		if head {
			buf.AppendString("  ")
		}
		buf.AppendString(ent.Message)
		head = true
	}
	for i, f := range final.fields {
		if i == 0 {
			if head {
				buf.AppendString("  ")
			}
		} else {
			buf.AppendByte(' ')
		}
		buf.AppendString(f.key)
		buf.AppendByte('=')
		buf.AppendString(f.value)
	}
	if ent.Stack != "" && e.cfg.StacktraceKey != "" {
		buf.AppendByte('\n')
		buf.AppendString(ent.Stack)
	}
	buf.AppendString(final.lineEnding())
	return buf, nil
}

func (e *consoleEncoder) lineEnding() string {
	if e.cfg.LineEnding != "" {
		return e.cfg.LineEnding
	}
	return zapcore.DefaultLineEnding
}

// consoleQuote keeps simple values bare (device_id=35020000001310000999) and
// quotes exactly those whose raw form would be ambiguous — empty, containing
// whitespace, '=', quotes or backslashes — so a record can never be split
// across lines by a value.
func consoleQuote(s string) string {
	if s == "" {
		return `""`
	}
	for i := 0; i < len(s); i++ {
		switch b := s[i]; {
		case b <= ' ', b == '"', b == '\\', b == '=', b == 0x7f:
			return strconv.Quote(s)
		}
	}
	return s
}

func consolePadLevel(level string) string {
	if len(level) >= consoleLevelWidth {
		return level
	}
	return level + strings.Repeat(" ", consoleLevelWidth-len(level))
}

// consolePadComponent aligns the module column. Byte-width padding is only
// valid for names that are pure ASCII; a wide-rune name is left untouched
// rather than misaligned.
func consolePadComponent(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] >= utf8.RuneSelf {
			return name
		}
	}
	if len(name) >= consoleComponentWidth {
		return name
	}
	return name + strings.Repeat(" ", consoleComponentWidth-len(name))
}

func renderConsoleObject(fields []consoleField) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.key+"="+f.value)
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// consolePrimitives satisfies PrimitiveArrayEncoder so the configured
// time/level/duration/caller encoders can render into a plain byte slice.
type consolePrimitives struct{ b []byte }

func (p *consolePrimitives) AppendBool(v bool)         { p.b = strconv.AppendBool(p.b, v) }
func (p *consolePrimitives) AppendByteString(v []byte) { p.b = append(p.b, consoleQuote(string(v))...) }
func (p *consolePrimitives) AppendComplex128(v complex128) {
	p.b = strconv.AppendFloat(p.b, real(v), 'g', -1, 64)
	p.b = append(p.b, '+')
	p.b = strconv.AppendFloat(p.b, imag(v), 'g', -1, 64)
	p.b = append(p.b, 'i')
}
func (p *consolePrimitives) AppendComplex64(v complex64) {
	p.AppendComplex128(complex128(v))
}
func (p *consolePrimitives) AppendFloat64(v float64) {
	p.b = strconv.AppendFloat(p.b, v, 'g', -1, 64)
}
func (p *consolePrimitives) AppendFloat32(v float32) {
	p.b = strconv.AppendFloat(p.b, float64(v), 'g', -1, 32)
}
func (p *consolePrimitives) AppendInt(v int)       { p.b = strconv.AppendInt(p.b, int64(v), 10) }
func (p *consolePrimitives) AppendInt64(v int64)   { p.b = strconv.AppendInt(p.b, v, 10) }
func (p *consolePrimitives) AppendInt32(v int32)   { p.b = strconv.AppendInt(p.b, int64(v), 10) }
func (p *consolePrimitives) AppendInt16(v int16)   { p.b = strconv.AppendInt(p.b, int64(v), 10) }
func (p *consolePrimitives) AppendInt8(v int8)     { p.b = strconv.AppendInt(p.b, int64(v), 10) }
func (p *consolePrimitives) AppendString(v string) { p.b = append(p.b, v...) }
func (p *consolePrimitives) AppendUint(v uint)     { p.b = strconv.AppendUint(p.b, uint64(v), 10) }
func (p *consolePrimitives) AppendUint64(v uint64) { p.b = strconv.AppendUint(p.b, v, 10) }
func (p *consolePrimitives) AppendUint32(v uint32) { p.b = strconv.AppendUint(p.b, uint64(v), 10) }
func (p *consolePrimitives) AppendUint16(v uint16) { p.b = strconv.AppendUint(p.b, uint64(v), 10) }
func (p *consolePrimitives) AppendUint8(v uint8)   { p.b = strconv.AppendUint(p.b, uint64(v), 10) }
func (p *consolePrimitives) AppendUintptr(v uintptr) {
	p.b = strconv.AppendUint(p.b, uint64(v), 10)
}

// consoleArrayEncoder renders array elements as space-separated values inside
// brackets. Array fields reach the encoder only from the inner core; the
// runtime core collapses them to a type name first.
type consoleArrayEncoder struct {
	cfg   zapcore.EncoderConfig
	items []string
}

func (a *consoleArrayEncoder) push(v string) { a.items = append(a.items, v) }

func (a *consoleArrayEncoder) AppendBool(v bool) { a.push(strconv.FormatBool(v)) }
func (a *consoleArrayEncoder) AppendByteString(v []byte) {
	a.push(consoleQuote(string(v)))
}
func (a *consoleArrayEncoder) AppendComplex128(v complex128) {
	a.push(strconv.FormatComplex(v, 'g', -1, 128))
}
func (a *consoleArrayEncoder) AppendComplex64(v complex64) {
	a.push(strconv.FormatComplex(complex128(v), 'g', -1, 64))
}
func (a *consoleArrayEncoder) AppendFloat64(v float64) {
	a.push(strconv.FormatFloat(v, 'g', -1, 64))
}
func (a *consoleArrayEncoder) AppendFloat32(v float32) {
	a.push(strconv.FormatFloat(float64(v), 'g', -1, 32))
}
func (a *consoleArrayEncoder) AppendInt(v int)     { a.push(strconv.Itoa(v)) }
func (a *consoleArrayEncoder) AppendInt64(v int64) { a.push(strconv.FormatInt(v, 10)) }
func (a *consoleArrayEncoder) AppendInt32(v int32) { a.push(strconv.FormatInt(int64(v), 10)) }
func (a *consoleArrayEncoder) AppendInt16(v int16) { a.push(strconv.FormatInt(int64(v), 10)) }
func (a *consoleArrayEncoder) AppendInt8(v int8)   { a.push(strconv.FormatInt(int64(v), 10)) }
func (a *consoleArrayEncoder) AppendString(v string) {
	a.push(consoleQuote(v))
}
func (a *consoleArrayEncoder) AppendUint(v uint)     { a.push(strconv.FormatUint(uint64(v), 10)) }
func (a *consoleArrayEncoder) AppendUint64(v uint64) { a.push(strconv.FormatUint(v, 10)) }
func (a *consoleArrayEncoder) AppendUint32(v uint32) { a.push(strconv.FormatUint(uint64(v), 10)) }
func (a *consoleArrayEncoder) AppendUint16(v uint16) { a.push(strconv.FormatUint(uint64(v), 10)) }
func (a *consoleArrayEncoder) AppendUint8(v uint8)   { a.push(strconv.FormatUint(uint64(v), 10)) }
func (a *consoleArrayEncoder) AppendUintptr(v uintptr) {
	a.push(strconv.FormatUint(uint64(v), 10))
}
func (a *consoleArrayEncoder) AppendDuration(v time.Duration) {
	a.push(encodeDurationWith(a.cfg, v))
}
func (a *consoleArrayEncoder) AppendTime(v time.Time) { a.push(encodeTimeWith(a.cfg, v)) }
func (a *consoleArrayEncoder) AppendArray(m zapcore.ArrayMarshaler) error {
	sub := &consoleArrayEncoder{cfg: a.cfg}
	if err := m.MarshalLogArray(sub); err != nil {
		return err
	}
	a.push("[" + strings.Join(sub.items, " ") + "]")
	return nil
}
func (a *consoleArrayEncoder) AppendObject(m zapcore.ObjectMarshaler) error {
	sub := &consoleEncoder{cfg: a.cfg}
	if err := m.MarshalLogObject(sub); err != nil {
		return err
	}
	a.push(renderConsoleObject(sub.fields))
	return nil
}
func (a *consoleArrayEncoder) AppendReflected(v interface{}) error {
	a.push(consoleQuote(fmt.Sprintf("%v", v)))
	return nil
}

func encodeTimeWith(cfg zapcore.EncoderConfig, v time.Time) string {
	if cfg.EncodeTime == nil {
		return v.Format(time.RFC3339Nano)
	}
	p := &consolePrimitives{}
	cfg.EncodeTime(v, p)
	return string(p.b)
}
func encodeDurationWith(cfg zapcore.EncoderConfig, v time.Duration) string {
	if cfg.EncodeDuration == nil {
		return v.String()
	}
	p := &consolePrimitives{}
	cfg.EncodeDuration(v, p)
	return string(p.b)
}
func encodeLevelWith(cfg zapcore.EncoderConfig, v zapcore.Level) string {
	if cfg.EncodeLevel == nil {
		return v.CapitalString()
	}
	p := &consolePrimitives{}
	cfg.EncodeLevel(v, p)
	return string(p.b)
}
func encodeCallerWith(cfg zapcore.EncoderConfig, v zapcore.EntryCaller) string {
	if cfg.EncodeCaller == nil {
		return v.TrimmedPath()
	}
	p := &consolePrimitives{}
	cfg.EncodeCaller(v, p)
	return string(p.b)
}
