package logging

import (
	"go.uber.org/zap/zapcore"
)

// canWriteUnchanged proves that normalization would neither remove, replace,
// append, nor truncate any field. Otherwise Write uses the full sanitizer.
// Six bytes per input byte conservatively bounds JSON escaping. Length checks
// precede multiplication, so attacker-controlled lengths cannot overflow.
// Invalid UTF-8 also fits this bound: the unchanged raw strings reach the
// same encoder as the slow path, which does not rewrite an uncut string.
func (c *runtimeCore) canWriteUnchanged(e zapcore.Entry, fields []zapcore.Field) bool {
	if c.truncated || e.Stack != "" || len(e.Message) > 1024/6 || len(e.LoggerName) > 256/6 || len(fields) > maxFields-len(c.bound) {
		return false
	}
	remaining := fieldBudget - c.used
	hasEvent := c.bound["event"]
	for _, f := range fields {
		if len(f.Key) > 128/6 || fixedKey(f.Key) || identityKey(f.Key) || (len(c.bound) > 0 && c.bound[f.Key]) {
			return false
		}
		policy := c.keyPolicies.lookup(f.Key)
		if policy.sensitive {
			return false
		}
		size := 6*len(f.Key) + 8
		switch f.Type {
		case zapcore.StringType:
			if policy.rule != stringFieldPlain || len(f.String) > 2048/6 {
				return false
			}
			size += 6 * len(f.String)
		case zapcore.BoolType, zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type, zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type, zapcore.UintptrType, zapcore.Float64Type, zapcore.Float32Type, zapcore.DurationType:
			size += 64
		default:
			return false
		}
		if remaining < 32 || size > remaining {
			return false
		}
		remaining -= size
		if f.Key == "event" {
			hasEvent = true
		}
	}
	return hasEvent
}
