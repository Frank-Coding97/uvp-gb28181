// Package mp4fixture 造测试用的「结构最小但完整」的 MP4 分片。
//
// ⛔ 为什么单独成一个包：mp4join 的逻辑测试（拼接正确性）与 recordcache 的
// 合并编排测试（多分片下载）都需要能播得动的 MP4 素材，而真实素材进不了仓库。
// 两处各写一份 builder 迟早在某处漂移（改了一处忘了另一处），所以只留这一份。
//
// ⛔ 表构造刻意不用 mp4join 的 encode*：否则等于「用自己证明自己」——
// 生产代码把偏移算错了，fixture 也跟着错，测试照样绿。
package mp4fixture

import (
	"bytes"
	"encoding/binary"
)

const (
	// SampleSize 是每个样本的字节数。
	SampleSize = 1024
	// Timescale 是 mvhd / mdhd 的时间基（1ms）。
	Timescale = 1000
	// Delta 是每个样本的时长（ms/sample）。
	Delta = 40
	// Fill 是 mdat 数据区的填充字节：断言「chunk 偏移指到了数据区」时用它。
	Fill = 0xAB
)

// Concat 顺序拼接若干字节片。
func Concat(parts ...[]byte) []byte {
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	out := make([]byte, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// RawBox 造一个 (size, type, payload) 的普通 box。
func RawBox(typ string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], typ)
	copy(out[8:], payload)
	return out
}

func fullBox(typ string, version byte, payload []byte) []byte {
	return RawBox(typ, append([]byte{version, 0, 0, 0}, payload...))
}

func mvhd(ts, dur uint32) []byte {
	p := make([]byte, 96)
	binary.BigEndian.PutUint32(p[12:16], ts)
	binary.BigEndian.PutUint32(p[16:20], dur)
	return fullBox("mvhd", 0, p)
}

func tkhd(dur uint32) []byte {
	p := make([]byte, 80)
	binary.BigEndian.PutUint32(p[12:16], 1)
	binary.BigEndian.PutUint32(p[20:24], dur)
	return fullBox("tkhd", 0, p)
}

func mdhd(ts, dur uint32) []byte {
	p := make([]byte, 20)
	binary.BigEndian.PutUint32(p[12:16], ts)
	binary.BigEndian.PutUint32(p[16:20], dur)
	return fullBox("mdhd", 0, p)
}

func hdlr(handler string) []byte {
	p := make([]byte, 24)
	// payload[4:8] → raw[16:20]，即 handler_type 的位置。
	copy(p[4:8], handler)
	return fullBox("hdlr", 0, p)
}

func stsd(tag string) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body[0:4], 1) // entry_count
	return fullBox("stsd", 0, Concat(body, RawBox("test", []byte(tag))))
}

type sttsEntry struct{ count, delta uint32 }

func mkStts(entries []sttsEntry) []byte {
	p := make([]byte, 4+len(entries)*8)
	binary.BigEndian.PutUint32(p[0:4], uint32(len(entries)))
	for i, e := range entries {
		binary.BigEndian.PutUint32(p[4+i*8:8+i*8], e.count)
		binary.BigEndian.PutUint32(p[8+i*8:12+i*8], e.delta)
	}
	return fullBox("stts", 0, p)
}

type stscEntry struct{ firstChunk, perChunk, descIndex uint32 }

func mkStsc(entries []stscEntry) []byte {
	p := make([]byte, 4+len(entries)*12)
	binary.BigEndian.PutUint32(p[0:4], uint32(len(entries)))
	for i, e := range entries {
		off := 4 + i*12
		binary.BigEndian.PutUint32(p[off:off+4], e.firstChunk)
		binary.BigEndian.PutUint32(p[off+4:off+8], e.perChunk)
		binary.BigEndian.PutUint32(p[off+8:off+12], e.descIndex)
	}
	return fullBox("stsc", 0, p)
}

func mkStszFixed(size, count uint32) []byte {
	p := make([]byte, 8)
	binary.BigEndian.PutUint32(p[0:4], size)
	binary.BigEndian.PutUint32(p[4:8], count)
	return fullBox("stsz", 0, p)
}

func mkStco(offsets []int64) []byte {
	p := make([]byte, 4+len(offsets)*4)
	binary.BigEndian.PutUint32(p[0:4], uint32(len(offsets)))
	for i, off := range offsets {
		binary.BigEndian.PutUint32(p[4+i*4:8+i*4], uint32(off))
	}
	return fullBox("stco", 0, p)
}

func mkStss(numbers []uint32) []byte {
	p := make([]byte, 4+len(numbers)*4)
	binary.BigEndian.PutUint32(p[0:4], uint32(len(numbers)))
	for i, n := range numbers {
		binary.BigEndian.PutUint32(p[4+i*4:8+i*4], n)
	}
	return fullBox("stss", 0, p)
}

// mkElst 造一条 edit list：整段映射到 media_time 0。
// ⛔ ZLM 录出的 MP4 每个 trak 都带它，且 segment_duration 决定**呈现时长**——
// fixture 漏了它，就漏测了「合并后时长停在第一片」这个坑。
func mkElst(segDur uint32) []byte {
	p := make([]byte, 16)
	binary.BigEndian.PutUint32(p[0:4], 1)            // entry_count
	binary.BigEndian.PutUint32(p[4:8], segDur)       // segment_duration → raw[16:20]
	binary.BigEndian.PutUint32(p[8:12], 0)           // media_time
	binary.BigEndian.PutUint32(p[12:16], 0x00010000) // media_rate 1.0
	return fullBox("elst", 0, p)
}

// SyncNumbers 生成「每 every 个样本一个同步样本」的 1-based 序号。
func SyncNumbers(count, every uint32) []uint32 {
	var out []uint32
	for i := uint32(1); i <= count; i += every {
		out = append(out, i)
	}
	return out
}

// Spec 描述要造的分片形状。
type Spec struct {
	Handlers  []string // 轨道类型，如 []string{"soun", "vide"}
	Tag       string   // stsd 里的编码标记（不同值表示编码参数不同）
	Samples   uint32   // 每条轨道的样本数
	Chunks    []int64  // 每条轨道的 chunk 起始（空则自动按顺序排）
	SyncEvery uint32   // >0 时生成 stss：每 N 个样本一个同步样本
}

// Fragment 造一个 ftyp + mdat + moov 的 MP4。每条轨道 1 个 chunk。
func Fragment(s Spec) []byte {
	ftyp := RawBox("ftyp", []byte("isom\x00\x00\x02\x00isomiso2mp41"))
	mdatDataStart := int64(len(ftyp)) + 8
	perTrack := int64(s.Samples) * SampleSize

	var traks [][]byte
	var payload []byte
	for i, handler := range s.Handlers {
		chunk := mdatDataStart + int64(i)*perTrack
		if len(s.Chunks) > i {
			chunk = s.Chunks[i]
		}
		traks = append(traks, buildTrak(handler, s.Tag, s.Samples, chunk, s.SyncEvery))
		payload = append(payload, bytes.Repeat([]byte{Fill}, int(perTrack))...)
	}
	mdat := RawBox("mdat", payload)
	moov := RawBox("moov", Concat(
		mvhd(Timescale, s.Samples*Delta),
		Concat(traks...),
	))
	return Concat(ftyp, mdat, moov)
}

func buildTrak(handler, tag string, samples uint32, chunk int64, syncEvery uint32) []byte {
	parts := [][]byte{
		stsd(tag),
		mkStts([]sttsEntry{{count: samples, delta: Delta}}),
		mkStsc([]stscEntry{{firstChunk: 1, perChunk: samples, descIndex: 1}}),
		mkStszFixed(SampleSize, samples),
		mkStco([]int64{chunk}),
	}
	if syncEvery > 0 {
		parts = append(parts, mkStss(SyncNumbers(samples, syncEvery)))
	}
	stbl := RawBox("stbl", Concat(parts...))
	mdia := RawBox("mdia", Concat(
		mdhd(Timescale, samples*Delta),
		hdlr(handler),
		RawBox("minf", stbl),
	))
	// 带上 edts/elst：真实素材都有，漏测就等于漏掉"时长停在第一片"这个坑。
	edts := RawBox("edts", mkElst(samples*Delta))
	return RawBox("trak", Concat(tkhd(samples*Delta), edts, mdia))
}
