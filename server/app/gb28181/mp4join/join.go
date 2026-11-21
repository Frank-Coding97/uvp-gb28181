package mp4join

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// 哨兵错误：调用方（缓存任务）据此决定"退回多文件下载"，
// 而不是把任务判失败 —— 合并只是锦上添花，不该让用户一个文件都拿不到。
var (
	// ErrNotRegularMP4 表示输入不是标准的非分片 MP4。
	ErrNotRegularMP4 = errors.New("mp4join: 不是标准 MP4")
	// ErrNoMoov 表示缺少 moov box。
	ErrNoMoov = errors.New("mp4join: 缺少 moov box")
	// ErrNoMdat 表示缺少 mdat box。
	ErrNoMdat = errors.New("mp4join: 缺少 mdat box")
	// ErrMultiMdat 表示一个分片里有多个 mdat box（当前不支持）。
	ErrMultiMdat = errors.New("mp4join: 分片含多个 mdat box")
	// ErrTrackMismatch 表示分片之间的轨道结构不一致。
	ErrTrackMismatch = errors.New("mp4join: 分片轨道结构不一致")
	// ErrCodecMismatch 表示分片之间的编码参数（stsd）不一致。
	ErrCodecMismatch = errors.New("mp4join: 分片编码参数不一致")
)

// topBox 是文件顶层的一个 box 位置。
type topBox struct {
	typ   string
	start int64
	size  int64
	hlen  int64 // 头部长度：8 或 16（64 位长度）
}

// track 是一个分片里的一条轨道。
type track struct {
	handler   string
	timescale uint32
	table     *sampleTable
}

// fragment 是一个分片在拼接过程中的全部已知信息。
type fragment struct {
	path string

	ftyp []byte
	moov *node // 只有第 1 片会作为输出模板复用

	// 源分片里 mdat 的**数据区**（不含 box 头）。
	mdatDataStart int64
	mdatDataSize  int64
	// 输出文件里该片数据区的起始偏移。
	newDataStart int64

	tracks []*track
}

// Concat 按时间顺序把 fragments 无损拼成 dst。
//
// 写盘是原子的：先写 dst+".tmp" 再 rename，失败不会留下半截文件。
//
// ⛔ 只有一片时不走拼接路径，直接复制：单片的 moov 本来就是对的，
// 重新序列化只是白白引入出错面。
func Concat(dst string, fragments []string) error {
	switch len(fragments) {
	case 0:
		return fmt.Errorf("mp4join: 没有分片")
	case 1:
		// 单片也走一次扫描：输出必须永远是"合法 MP4"，不能把空文件/垃圾字节
		// 原样复制出去当合并结果（调用方拿到的是一个打不开的文件）。
		if _, err := scanFragment(fragments[0]); err != nil {
			return err
		}
		return copyFile(dst, fragments[0])
	}
	metas := make([]*fragment, 0, len(fragments))
	for _, path := range fragments {
		meta, err := scanFragment(path)
		if err != nil {
			return err
		}
		metas = append(metas, meta)
	}
	if err := validate(metas); err != nil {
		return err
	}
	return writeJoined(dst, metas)
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("mp4join: 打开分片失败: %w", err)
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// scanTopBoxes 只读顶层 box 的头，不把 mdat 读进内存。
func scanTopBoxes(f *os.File, size int64) ([]topBox, error) {
	var out []topBox
	var off int64
	for off+8 <= size {
		var hdr [16]byte
		if _, err := f.ReadAt(hdr[:8], off); err != nil {
			return nil, err
		}
		sz := uint64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := string(hdr[4:8])
		hlen := int64(8)
		switch {
		case sz == 1:
			if _, err := f.ReadAt(hdr[:16], off); err != nil {
				return nil, err
			}
			sz = binary.BigEndian.Uint64(hdr[8:16])
			hlen = 16
		case sz == 0:
			sz = uint64(size - off)
		}
		if int64(sz) < hlen || off+int64(sz) > size {
			return nil, fmt.Errorf("mp4join: box %q 长度非法（%d，文件 %d 字节）", typ, sz, size)
		}
		out = append(out, topBox{typ: typ, start: off, size: int64(sz), hlen: hlen})
		if sz == 0 {
			break
		}
		off += int64(sz)
	}
	return out, nil
}

func scanFragment(path string) (*fragment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mp4join: 打开分片 %s 失败: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("mp4join: 分片 %s 是空文件", path)
	}
	boxes, err := scanTopBoxes(f, info.Size())
	if err != nil {
		return nil, fmt.Errorf("mp4join: %s: %w", path, err)
	}

	frag := &fragment{path: path}
	var moov *topBox
	mdatCount := 0
	for i := range boxes {
		b := boxes[i]
		switch b.typ {
		case "ftyp":
			raw := make([]byte, b.size)
			if _, err := f.ReadAt(raw, b.start); err != nil {
				return nil, err
			}
			frag.ftyp = raw
		case "mdat":
			mdatCount++
			// 只记数据区：输出端会重新写一个统一的 mdat 头。
			frag.mdatDataStart = b.start + b.hlen
			frag.mdatDataSize = b.size - b.hlen
		case "moov":
			moov = &boxes[i]
		case "moof", "sidx":
			return nil, fmt.Errorf("%w: %s 含 %s（分片 MP4）", ErrNotRegularMP4, path, b.typ)
		}
	}
	switch {
	case frag.ftyp == nil:
		return nil, fmt.Errorf("%w: %s 无 ftyp", ErrNotRegularMP4, path)
	case mdatCount == 0:
		return nil, fmt.Errorf("%w: %s", ErrNoMdat, path)
	case mdatCount > 1:
		return nil, fmt.Errorf("%w: %s", ErrMultiMdat, path)
	case moov == nil:
		return nil, fmt.Errorf("%w: %s", ErrNoMoov, path)
	}

	raw := make([]byte, moov.size)
	if _, err := f.ReadAt(raw, moov.start); err != nil {
		return nil, err
	}
	nodes, err := parseChildren(raw)
	if err != nil {
		return nil, fmt.Errorf("mp4join: %s: %w", path, err)
	}
	if len(nodes) != 1 || nodes[0].typ != "moov" {
		return nil, fmt.Errorf("%w: %s 的 moov 结构异常", ErrNoMoov, path)
	}
	frag.moov = nodes[0]
	if err := readTracks(frag); err != nil {
		return nil, fmt.Errorf("mp4join: %s: %w", path, err)
	}
	return frag, nil
}

func readTracks(frag *fragment) error {
	for _, trak := range childrenOf(frag.moov, "trak") {
		mdia := childOf(trak, "mdia")
		mdhd := childOf(mdia, "mdhd")
		hdlr := childOf(mdia, "hdlr")
		stbl := childOf(childOf(mdia, "minf"), "stbl")
		if mdhd == nil || hdlr == nil || stbl == nil {
			return fmt.Errorf("trak 结构不完整（缺 mdhd/hdlr/stbl）")
		}
		ts, err := mdhdTimeScale(mdhd.raw)
		if err != nil {
			return err
		}
		table, err := parseSampleTable(stbl)
		if err != nil {
			return err
		}
		frag.tracks = append(frag.tracks, &track{
			handler:   handlerType(hdlr.raw),
			timescale: ts,
			table:     table,
		})
	}
	if len(frag.tracks) == 0 {
		return fmt.Errorf("moov 里没有 trak")
	}
	return nil
}

// validate 校验各分片能不能拼在一张表里。
//
// ⛔ 这些检查不是"防御性编程"而是必须的：不满足时硬拼出来的文件
// 尺寸上看着正常、播放器却会在片边界花屏或卡死，比直接失败更糟。
func validate(metas []*fragment) error {
	first := metas[0]
	for _, m := range metas[1:] {
		if len(m.tracks) != len(first.tracks) {
			return fmt.Errorf("%w: 轨道数 %d != %d", ErrTrackMismatch, len(m.tracks), len(first.tracks))
		}
		for i := range m.tracks {
			a, b := first.tracks[i], m.tracks[i]
			if a.handler != b.handler {
				return fmt.Errorf("%w: 第 %d 条轨道类型 %q != %q", ErrTrackMismatch, i, b.handler, a.handler)
			}
			if a.timescale != b.timescale {
				return fmt.Errorf("%w: 第 %d 条轨道 timescale %d != %d", ErrTrackMismatch, i, b.timescale, a.timescale)
			}
			if !bytes.Equal(a.table.stsd, b.table.stsd) {
				return fmt.Errorf("%w: 第 %d 条轨道(%s)的编码参数不同", ErrCodecMismatch, i, a.handler)
			}
			// stsz 的两种表示（定长 / 逐样本）必须一致，否则没法合成一张表。
			if (a.table.fixedSize == 0) != (b.table.fixedSize == 0) {
				return fmt.Errorf("%w: 第 %d 条轨道 stsz 表示不一致", ErrTrackMismatch, i)
			}
			if a.table.fixedSize != 0 && a.table.fixedSize != b.table.fixedSize {
				return fmt.Errorf("%w: 第 %d 条轨道定长样本大小不一致", ErrTrackMismatch, i)
			}
		}
	}
	return nil
}

// mergedTrack 是多片合并后的单条轨道。
type mergedTrack struct {
	handler     string
	timescale   uint32
	stsd        []byte
	co64        bool
	hasCtts     bool
	cttsVer     byte
	hasStss     bool

	// 合并后的表
	stts        []sttsEntry
	stsc        []stscEntry
	fixedSize   uint32
	sizes       []uint32
	sampleCount uint32
	offsets     []int64
	ctts        []cttsEntry
	stss        []uint32

	duration uint64 // media timescale 单位
}

func mergeTracks(metas []*fragment) []*mergedTrack {
	count := len(metas[0].tracks)
	out := make([]*mergedTrack, count)
	for i := 0; i < count; i++ {
		head := metas[0].tracks[i].table
		m := &mergedTrack{
			handler:   metas[0].tracks[i].handler,
			timescale: metas[0].tracks[i].timescale,
			stsd:      head.stsd,
			co64:      head.co64,
			hasCtts:   head.hasCtts,
			cttsVer:   head.cttsVer,
			hasStss:   head.hasStss,
			fixedSize: head.fixedSize,
		}
		var sampleBase, chunkBase uint32
		for _, frag := range metas {
			tb := frag.tracks[i].table
			m.stts = appendStts(m.stts, tb.stts)
			for _, e := range tb.stsc {
				m.stsc = append(m.stsc, stscEntry{
					// first_chunk 是 1-based 的 chunk 序号：前面各片的 chunk 数
					// 就是本片在合并表里的序号偏移。
					firstChunk: e.firstChunk + chunkBase,
					perChunk:   e.perChunk,
					descIndex:  e.descIndex,
				})
			}
			for _, off := range tb.offsets {
				// ⛔ 偏移必须跟着 mdat 的新落点走：输出端把所有分片的数据区
				// 并在同一个 mdat 里，不重算就会得到"文件能播、seek 全乱"的结果。
				m.offsets = append(m.offsets, off-frag.mdatDataStart+frag.newDataStart)
			}
			m.sizes = append(m.sizes, tb.sizes...)
			if tb.hasCtts {
				m.ctts = append(m.ctts, tb.ctts...)
			}
			for _, n := range tb.stss {
				// 同步样本序号是 1-based 的全表序号，同样要加偏移。
				m.stss = append(m.stss, n+sampleBase)
			}
			m.sampleCount += tb.sampleCount
			sampleBase += tb.sampleCount
			chunkBase += uint32(len(tb.offsets))
		}
		m.duration = sttsDuration(m.stts)
		out[i] = m
	}
	return out
}

// appendStts 追加 stts run，并合并相邻同 delta 的 run（表更紧凑，语义不变）。
func appendStts(dst, add []sttsEntry) []sttsEntry {
	for _, e := range add {
		if e.count == 0 {
			continue
		}
		if n := len(dst); n > 0 && dst[n-1].delta == e.delta {
			dst[n-1].count += e.count
			continue
		}
		dst = append(dst, e)
	}
	return dst
}

func sttsDuration(entries []sttsEntry) uint64 {
	var total uint64
	for _, e := range entries {
		total += uint64(e.count) * uint64(e.delta)
	}
	return total
}

func writeJoined(dst string, metas []*fragment) error {
	// ⛔ 输出只写**一个** mdat：虽然规范允许多个，但把各分片的数据区并在同一个
	// mdat 里更标准、兼容性更好（部分解析器只认第一个 mdat）。
	dataLen := int64(0)
	for _, m := range metas {
		dataLen += m.mdatDataSize
	}
	headerLen := int64(8)
	if dataLen+8 > int64(^uint32(0)) {
		headerLen = 16 // 超过 32 位范围改用 largesize
	}

	// 输出布局：ftyp + [mdat 头 + Σ 数据区] + moov。先算各片数据区的新落点。
	offset := int64(len(metas[0].ftyp)) + headerLen
	for _, m := range metas {
		m.newDataStart = offset
		offset += m.mdatDataSize
	}

	merged := mergeTracks(metas)
	template := metas[0]
	// ⛔ 模板就是第 1 片的 moov：所有分片的 moov 除 sample 表和时长外都相同
	// （编码参数已在 validate 里验过一致），所以改一个就够。
	if err := applyMerged(template.moov, merged); err != nil {
		return err
	}

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	discard := func(err error) error {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := out.Write(template.ftyp); err != nil {
		return discard(err)
	}
	if err := writeMdatHeader(out, headerLen, dataLen); err != nil {
		return discard(err)
	}
	for _, m := range metas {
		src, err := os.Open(m.path)
		if err != nil {
			return discard(err)
		}
		if _, err := src.Seek(m.mdatDataStart, io.SeekStart); err != nil {
			src.Close()
			return discard(err)
		}
		// 流式拷贝数据区：单片几十 MB，不整个读进内存。
		if _, err := io.CopyN(out, src, m.mdatDataSize); err != nil {
			src.Close()
			return discard(err)
		}
		src.Close()
	}
	if _, err := out.Write(template.moov.encode()); err != nil {
		return discard(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func writeMdatHeader(w io.Writer, headerLen, dataLen int64) error {
	if headerLen == 16 {
		var hdr [16]byte
		binary.BigEndian.PutUint32(hdr[0:4], 1)
		copy(hdr[4:8], "mdat")
		binary.BigEndian.PutUint64(hdr[8:16], uint64(dataLen+16))
		_, err := w.Write(hdr[:])
		return err
	}
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(dataLen+8))
	copy(hdr[4:8], "mdat")
	_, err := w.Write(hdr[:])
	return err
}

func applyMerged(moov *node, merged []*mergedTrack) error {
	movieTS, err := mvhdTimeScale(moov)
	if err != nil {
		return err
	}
	if movieTS == 0 {
		movieTS = 1000
	}
	traks := childrenOf(moov, "trak")
	if len(traks) != len(merged) {
		return fmt.Errorf("%w: trak 数 %d != 合并结果 %d", ErrTrackMismatch, len(traks), len(merged))
	}

	var maxMovieDur uint64
	for i, trak := range traks {
		m := merged[i]
		mdia := childOf(trak, "mdia")
		stbl := childOf(childOf(mdia, "minf"), "stbl")
		if stbl == nil {
			return fmt.Errorf("mp4join: trak 缺少 stbl")
		}

		stbl.replaceChild("stts", encodeStts(m.stts))
		stbl.replaceChild("stsc", encodeStsc(m.stsc))
		stbl.replaceChild("stsz", encodeStsz(m.fixedSize, m.sizes, m.sampleCount))
		kind, raw := encodeChunkOffsets(m.offsets, m.co64)
		stbl.replaceChild(kind, raw)
		// stco / co64 只能存在一个：换成另一种表示时必须把旧的摘掉。
		if kind == "stco" {
			stbl.removeChild("co64")
		} else {
			stbl.removeChild("stco")
		}
		// ctts / stss 是可选表：模板里有而合并结果没有时要摘掉，反之要补上。
		if m.hasCtts {
			stbl.replaceChild("ctts", encodeCtts(m.cttsVer, m.ctts))
		} else {
			stbl.removeChild("ctts")
		}
		if m.hasStss {
			stbl.replaceChild("stss", encodeStss(m.stss))
		} else {
			stbl.removeChild("stss")
		}

		// 时长：mdhd 用 media timescale，tkhd 用 movie timescale。
		mediaTS := uint64(m.timescale)
		if mediaTS == 0 {
			mediaTS = 1000
		}
		movieDur := m.duration
		if mediaTS != uint64(movieTS) {
			movieDur = m.duration * uint64(movieTS) / mediaTS
		}
		if mdhd := childOf(mdia, "mdhd"); mdhd != nil {
			if err := patchMdhdDuration(mdhd.raw, m.duration); err != nil {
				return err
			}
		}
		if tkhd := childOf(trak, "tkhd"); tkhd != nil {
			if err := patchTkhdDuration(tkhd.raw, movieDur); err != nil {
				return err
			}
		}
		// ⛔ edit list 也要跟着改：播放器/ffmpeg 报的**呈现时长**以它为准。
		// 漏了这处，其它字段全对，时长却停在第一片（实测踩过）。
		if edts := childOf(trak, "edts"); edts != nil {
			if elst := childOf(edts, "elst"); elst != nil {
				if err := patchElstDuration(elst.raw, movieDur); err != nil {
					return err
				}
			}
		}
		if movieDur > maxMovieDur {
			maxMovieDur = movieDur
		}
	}

	mvhd := childOf(moov, "mvhd")
	if mvhd == nil {
		return fmt.Errorf("mp4join: moov 缺少 mvhd")
	}
	return patchMvhdDuration(mvhd.raw, maxMovieDur)
}
