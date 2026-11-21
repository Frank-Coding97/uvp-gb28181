// Package mp4join 把同一录制器产出的多个 MP4 分片无损拼成一个 MP4。
//
// 背景：GB28181 下载模式下平台单会话有墙钟上限，超长录像必须切成多个会话
// （每片一次会话 ⇒ ZLM 上一条独立 stream ⇒ 一个独立 MP4）。用户要的是
// 「一段录像一个文件」，所以在任务成功后做一次离线拼接。
//
// ⛔ 不重编码、不依赖 ffmpeg：交付形态是单个二进制，不能要求现场装 ffmpeg。
//
// 做法（关键取舍）：
//
//	ftyp(第 1 片) + mdat(第 1 片) + mdat(第 2 片) + … + moov(重建)
//
// 为什么不把 sample 数据重排成"全局交织"：不必要。每个分片内部本来就是
// 音视频交织的，分片之间时间连续，所以"按分片顺序排列"在时间上就是顺序的。
// 重排只会多一次几十 MB 的拷贝和成倍的出错面。MP4 规范允许多个 mdat box。
//
// 局限（不满足时返回错误，调用方退回"多文件下载"）：
//   - 只支持标准 MP4（含 moof/sidx 的 fMP4 直接拒绝）
//   - 每个分片只允许一个 mdat box
//   - 各分片的轨道数/轨道类型/stsd（编码参数）必须一致
package mp4join

import (
	"encoding/binary"
	"fmt"
)

// ---------- box 树 ----------

// containerTypes 是「载荷是子 box」的类型白名单。
//
// ⛔ 只列我们**需要读进内部**的那条路径（moov → trak → {edts} / mdia → minf → stbl）。
// 把别的 box（udta、dinf…）也当容器，一旦它们的载荷不是规整的 box 序列
// （udta 里常有自由数据）解析就会失败；而我们本来就只需要原样搬运它们。
//
// ⛔ edts 必须在名单里：它的载荷一定是 elst，而 elst.segment_duration 决定
// ffmpeg/播放器报出的**呈现时长**（见 patchElstDuration）。不解析它就只能当叶子
// 整块搬运，那边的时长永远停在第一片。
var containerTypes = map[string]bool{
	"moov": true,
	"trak": true,
	"edts": true,
	"mdia": true,
	"minf": true,
	"stbl": true,
}

// node 是解析后的一个 box。
type node struct {
	typ  string
	raw  []byte  // 叶子：完整原始字节（含 box 头）
	kids []*node // 容器：子 box
}

func (n *node) leaf() bool { return n.kids == nil }

// parseChildren 解析一段连续的 box 序列，b 是容器 box 的**载荷**（已去掉头部）。
func parseChildren(b []byte) ([]*node, error) {
	var out []*node
	for len(b) > 0 {
		if len(b) < 8 {
			// 末尾残字节：录制器偶尔会留 padding，容忍。
			break
		}
		size := uint64(binary.BigEndian.Uint32(b[0:4]))
		typ := string(b[4:8])
		hdr := 8
		switch {
		case size == 1:
			if len(b) < 16 {
				return nil, fmt.Errorf("mp4join: box %q 头部被截断", typ)
			}
			size = binary.BigEndian.Uint64(b[8:16])
			hdr = 16

		case size == 0:
			size = uint64(len(b))
		}
		if size < uint64(hdr) || size > uint64(len(b)) {
			return nil, fmt.Errorf("mp4join: box %q 长度非法(%d，剩余 %d)", typ, size, len(b))
		}
		body := b[:size]
		cur := &node{typ: typ}
		if containerTypes[typ] {
			kids, err := parseChildren(body[hdr:])
			if err != nil {
				return nil, err
			}
			cur.kids = kids
		} else {
			cur.raw = body
		}
		out = append(out, cur)
		b = b[size:]
	}
	return out, nil
}

// encode 把节点写回字节。容器会重算自身长度（子 box 换了长度必须重算）。
func (n *node) encode() []byte {
	if n.leaf() {
		return n.raw
	}
	out := make([]byte, 0, n.encodedLen())
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(n.encodedLen()))
	copy(hdr[4:8], n.typ)
	out = append(out, hdr[:]...)
	for _, k := range n.kids {
		out = append(out, k.encode()...)
	}
	return out
}

func (n *node) encodedLen() int {
	if n.leaf() {
		return len(n.raw)
	}
	total := 8
	for _, k := range n.kids {
		total += k.encodedLen()
	}
	return total
}

func childOf(n *node, typ string) *node {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.typ == typ {
			return k
		}
	}
	return nil
}

func childrenOf(n *node, typ string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for _, k := range n.kids {
		if k.typ == typ {
			out = append(out, k)
		}
	}
	return out
}

// replaceChild 用新的叶子 box 替换容器里的同类型子 box（没有则追加）。
func (n *node) replaceChild(typ string, raw []byte) {
	for i, k := range n.kids {
		if k.typ == typ {
			n.kids[i] = &node{typ: typ, raw: raw}
			return
		}
	}
	n.kids = append(n.kids, &node{typ: typ, raw: raw})
}

func (n *node) removeChild(typ string) {
	kept := n.kids[:0]
	for _, k := range n.kids {
		if k.typ != typ {
			kept = append(kept, k)
		}
	}
	n.kids = kept
}

// ---------- stbl 的表 ----------

type sttsEntry struct {
	count uint32
	delta uint32
}

type stscEntry struct {
	firstChunk uint32
	perChunk   uint32
	descIndex  uint32
}

type cttsEntry struct {
	count  uint32
	offset int32
}

// sampleTable 是一个 track 在**一个分片**里的 sample 表。
//
// 解析时按 stbl 里的原始表示保留：定长 sample（stsz.sample_size != 0）与
// 逐样本长度（sizes）是两种互斥的表示，合并要求所有分片用同一种。
type sampleTable struct {
	stsd        []byte
	stts        []sttsEntry
	stsc        []stscEntry
	fixedSize   uint32   // stsz.sample_size；非 0 表示定长
	sizes       []uint32 // 定长时为空
	sampleCount uint32
	offsets     []int64 // chunk 偏移（相对本文件绝对偏移）
	co64        bool
	hasCtts     bool
	cttsVer     byte
	ctts        []cttsEntry
	hasStss     bool
	stss        []uint32 // 1-based 同步样本序号
}

func errShortBox(typ string) error {
	return fmt.Errorf("mp4join: box %s 长度不足", typ)
}

func parseSampleTable(stbl *node) (*sampleTable, error) {
	if stbl == nil {
		return nil, fmt.Errorf("mp4join: 缺少 stbl")
	}
	st := &sampleTable{}
	stsd := childOf(stbl, "stsd")
	if stsd == nil {
		return nil, fmt.Errorf("mp4join: stbl 缺少 stsd")
	}
	st.stsd = stsd.raw

	var err error
	if node := childOf(stbl, "stts"); node != nil {
		if st.stts, err = parseStts(node.raw); err != nil {
			return nil, err
		}
	}
	if node := childOf(stbl, "stsc"); node != nil {
		if st.stsc, err = parseStsc(node.raw); err != nil {
			return nil, err
		}
	}
	if node := childOf(stbl, "stsz"); node != nil {
		if st.fixedSize, st.sampleCount, st.sizes, err = parseStsz(node.raw); err != nil {
			return nil, err
		}
	}
	if st.sampleCount == 0 {
		// stsz 缺失时回退到 stts 求和：样本数是拼接的关键输入，不能是 0。
		var sum uint32
		for _, e := range st.stts {
			sum += e.count
		}
		st.sampleCount = sum
	}
	switch {
	case childOf(stbl, "stco") != nil:
		if st.offsets, err = parseStco(childOf(stbl, "stco").raw); err != nil {
			return nil, err
		}
	case childOf(stbl, "co64") != nil:
		st.co64 = true
		if st.offsets, err = parseCo64(childOf(stbl, "co64").raw); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("mp4join: stbl 既无 stco 也无 co64")
	}
	if node := childOf(stbl, "ctts"); node != nil {
		st.hasCtts = true
		st.cttsVer = node.raw[8]
		if st.ctts, err = parseCtts(node.raw); err != nil {
			return nil, err
		}
	}
	if node := childOf(stbl, "stss"); node != nil {
		st.hasStss = true
		if st.stss, err = parseStss(node.raw); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func parseStts(raw []byte) ([]sttsEntry, error) {
	n, err := tableEntryCount(raw, "stts")
	if err != nil {
		return nil, err
	}
	if len(raw) < 16+n*8 {
		return nil, errShortBox("stts")
	}
	out := make([]sttsEntry, 0, n)
	for i, off := 0, 16; i < n; i, off = i+1, off+8 {
		out = append(out, sttsEntry{
			count: binary.BigEndian.Uint32(raw[off : off+4]),
			delta: binary.BigEndian.Uint32(raw[off+4 : off+8]),
		})
	}
	return out, nil
}

func parseStsc(raw []byte) ([]stscEntry, error) {
	n, err := tableEntryCount(raw, "stsc")
	if err != nil {
		return nil, err
	}
	if len(raw) < 16+n*12 {
		return nil, errShortBox("stsc")
	}
	out := make([]stscEntry, 0, n)
	for i, off := 0, 16; i < n; i, off = i+1, off+12 {
		out = append(out, stscEntry{
			firstChunk: binary.BigEndian.Uint32(raw[off : off+4]),
			perChunk:   binary.BigEndian.Uint32(raw[off+4 : off+8]),
			descIndex:  binary.BigEndian.Uint32(raw[off+8 : off+12]),
		})
	}
	return out, nil
}

// parseStsz 返回 (定长样本大小, 样本数, 逐样本大小)。定长时 sizes 为 nil。
func parseStsz(raw []byte) (fixed, count uint32, sizes []uint32, err error) {
	if len(raw) < 20 {
		return 0, 0, nil, errShortBox("stsz")
	}
	fixed = binary.BigEndian.Uint32(raw[12:16])
	count = binary.BigEndian.Uint32(raw[16:20])
	if fixed != 0 {
		return fixed, count, nil, nil
	}
	if len(raw) < 20+int(count)*4 {
		return 0, 0, nil, errShortBox("stsz")
	}
	sizes = make([]uint32, 0, count)
	for i := 0; i < int(count); i++ {
		sizes = append(sizes, binary.BigEndian.Uint32(raw[20+i*4:24+i*4]))
	}
	return 0, count, sizes, nil
}

func parseStco(raw []byte) ([]int64, error) {
	n, err := tableEntryCount(raw, "stco")
	if err != nil {
		return nil, err
	}
	if len(raw) < 16+n*4 {
		return nil, errShortBox("stco")
	}
	out := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, int64(binary.BigEndian.Uint32(raw[16+i*4:20+i*4])))
	}
	return out, nil
}

func parseCo64(raw []byte) ([]int64, error) {
	n, err := tableEntryCount(raw, "co64")
	if err != nil {
		return nil, err
	}
	if len(raw) < 16+n*8 {
		return nil, errShortBox("co64")
	}
	out := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		v := binary.BigEndian.Uint64(raw[16+i*8 : 24+i*8])
		out = append(out, int64(v))
	}
	return out, nil
}

func parseCtts(raw []byte) ([]cttsEntry, error) {
	n, err := tableEntryCount(raw, "ctts")
	if err != nil {
		return nil, err
	}
	if len(raw) < 16+n*8 {
		return nil, errShortBox("ctts")
	}
	out := make([]cttsEntry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, cttsEntry{
			count:  binary.BigEndian.Uint32(raw[16+i*8 : 20+i*8]),
			offset: int32(binary.BigEndian.Uint32(raw[20+i*8 : 24+i*8])),
		})
	}
	return out, nil
}

func parseStss(raw []byte) ([]uint32, error) {
	n, err := tableEntryCount(raw, "stss")
	if err != nil {
		return nil, err
	}
	if len(raw) < 16+n*4 {
		return nil, errShortBox("stss")
	}
	out := make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, binary.BigEndian.Uint32(raw[16+i*4:20+i*4]))
	}
	return out, nil
}

// tableEntryCount 读 full box 的 entry_count（统一位于偏移 12）。
func tableEntryCount(raw []byte, typ string) (int, error) {
	if len(raw) < 16 {
		return 0, errShortBox(typ)
	}
	return int(binary.BigEndian.Uint32(raw[12:16])), nil
}

// ---------- 表的编码 ----------

func encodeStts(entries []sttsEntry) []byte {
	raw := make([]byte, 16+len(entries)*8)
	binary.BigEndian.PutUint32(raw[0:4], uint32(len(raw)))
	copy(raw[4:8], "stts")
	binary.BigEndian.PutUint32(raw[12:16], uint32(len(entries)))
	for i, e := range entries {
		binary.BigEndian.PutUint32(raw[16+i*8:20+i*8], e.count)
		binary.BigEndian.PutUint32(raw[20+i*8:24+i*8], e.delta)
	}
	return raw
}

func encodeStsc(entries []stscEntry) []byte {
	raw := make([]byte, 16+len(entries)*12)
	binary.BigEndian.PutUint32(raw[0:4], uint32(len(raw)))
	copy(raw[4:8], "stsc")
	binary.BigEndian.PutUint32(raw[12:16], uint32(len(entries)))
	for i, e := range entries {
		off := 16 + i*12
		binary.BigEndian.PutUint32(raw[off:off+4], e.firstChunk)
		binary.BigEndian.PutUint32(raw[off+4:off+8], e.perChunk)
		binary.BigEndian.PutUint32(raw[off+8:off+12], e.descIndex)
	}
	return raw
}

func encodeStsz(fixed uint32, sizes []uint32, sampleCount uint32) []byte {
	if fixed != 0 {
		raw := make([]byte, 20)
		binary.BigEndian.PutUint32(raw[0:4], 20)
		copy(raw[4:8], "stsz")
		binary.BigEndian.PutUint32(raw[12:16], fixed)
		binary.BigEndian.PutUint32(raw[16:20], sampleCount)
		return raw
	}
	raw := make([]byte, 20+len(sizes)*4)
	binary.BigEndian.PutUint32(raw[0:4], uint32(len(raw)))
	copy(raw[4:8], "stsz")
	binary.BigEndian.PutUint32(raw[16:20], uint32(len(sizes)))
	for i, s := range sizes {
		binary.BigEndian.PutUint32(raw[20+i*4:24+i*4], s)
	}
	return raw
}

// encodeChunkOffsets 生成 stco 或 co64（偏移超过 32 位范围时用后者）。
func encodeChunkOffsets(offsets []int64, preferCo64 bool) (string, []byte) {
	useCo64 := preferCo64
	if !useCo64 {
		for _, off := range offsets {
			if off > int64(^uint32(0)) {
				useCo64 = true
				break
			}
		}
	}
	if useCo64 {
		raw := make([]byte, 16+len(offsets)*8)
		binary.BigEndian.PutUint32(raw[0:4], uint32(len(raw)))
		copy(raw[4:8], "co64")
		binary.BigEndian.PutUint32(raw[12:16], uint32(len(offsets)))
		for i, off := range offsets {
			binary.BigEndian.PutUint64(raw[16+i*8:24+i*8], uint64(off))
		}
		return "co64", raw
	}
	raw := make([]byte, 16+len(offsets)*4)
	binary.BigEndian.PutUint32(raw[0:4], uint32(len(raw)))
	copy(raw[4:8], "stco")
	binary.BigEndian.PutUint32(raw[12:16], uint32(len(offsets)))
	for i, off := range offsets {
		binary.BigEndian.PutUint32(raw[16+i*4:20+i*4], uint32(off))
	}
	return "stco", raw
}

func encodeCtts(version byte, entries []cttsEntry) []byte {
	raw := make([]byte, 16+len(entries)*8)
	binary.BigEndian.PutUint32(raw[0:4], uint32(len(raw)))
	copy(raw[4:8], "ctts")
	raw[8] = version
	binary.BigEndian.PutUint32(raw[12:16], uint32(len(entries)))
	for i, e := range entries {
		binary.BigEndian.PutUint32(raw[16+i*8:20+i*8], e.count)
		binary.BigEndian.PutUint32(raw[20+i*8:24+i*8], uint32(e.offset))
	}
	return raw
}

func encodeStss(numbers []uint32) []byte {
	raw := make([]byte, 16+len(numbers)*4)
	binary.BigEndian.PutUint32(raw[0:4], uint32(len(raw)))
	copy(raw[4:8], "stss")
	binary.BigEndian.PutUint32(raw[12:16], uint32(len(numbers)))
	for i, n := range numbers {
		binary.BigEndian.PutUint32(raw[16+i*4:20+i*4], n)
	}
	return raw
}

// ---------- 头部字段读取 ----------

// mvhdTimeScale 读 movie timescale（mvhd 的 duration 与各 tkhd.duration 都用它）。
func mvhdTimeScale(moov *node) (uint32, error) {
	mvhd := childOf(moov, "mvhd")
	if mvhd == nil {
		return 0, fmt.Errorf("mp4join: moov 缺少 mvhd")
	}
	raw := mvhd.raw
	if len(raw) < 24 {
		return 0, errShortBox("mvhd")
	}
	if raw[8] == 1 {
		if len(raw) < 32 {
			return 0, errShortBox("mvhd")
		}
		return binary.BigEndian.Uint32(raw[28:32]), nil
	}
	return binary.BigEndian.Uint32(raw[20:24]), nil
}

func mdhdTimeScale(raw []byte) (uint32, error) {
	if len(raw) < 24 {
		return 0, errShortBox("mdhd")
	}
	if raw[8] == 1 {
		if len(raw) < 32 {
			return 0, errShortBox("mdhd")
		}
		return binary.BigEndian.Uint32(raw[28:32]), nil
	}
	return binary.BigEndian.Uint32(raw[20:24]), nil
}

// handlerType 读 hdlr 的 handler_type（vide / soun / …）。
func handlerType(raw []byte) string {
	if len(raw) < 20 {
		return ""
	}
	return string(raw[16:20])
}

// patchMdhdDuration 原地更新 mdhd.duration（单位是 media timescale）。
func patchMdhdDuration(raw []byte, d uint64) error {
	if len(raw) < 12 {
		return errShortBox("mdhd")
	}
	if raw[8] == 1 {
		if len(raw) < 40 {
			return errShortBox("mdhd")
		}
		binary.BigEndian.PutUint64(raw[32:40], d)
		return nil
	}
	if len(raw) < 28 {
		return errShortBox("mdhd")
	}
	binary.BigEndian.PutUint32(raw[24:28], clampUint32(d))
	return nil
}

// patchTkhdDuration 原地更新 tkhd.duration（单位是 movie timescale）。
func patchTkhdDuration(raw []byte, d uint64) error {
	if len(raw) < 12 {
		return errShortBox("tkhd")
	}
	if raw[8] == 1 {
		if len(raw) < 44 {
			return errShortBox("tkhd")
		}
		binary.BigEndian.PutUint64(raw[36:44], d)
		return nil
	}
	if len(raw) < 32 {
		return errShortBox("tkhd")
	}
	binary.BigEndian.PutUint32(raw[28:32], clampUint32(d))
	return nil
}

// patchMvhdDuration 原地更新 mvhd.duration（单位是 movie timescale）。
func patchMvhdDuration(raw []byte, d uint64) error {
	if len(raw) < 12 {
		return errShortBox("mvhd")
	}
	if raw[8] == 1 {
		if len(raw) < 40 {
			return errShortBox("mvhd")
		}
		binary.BigEndian.PutUint64(raw[32:40], d)
		return nil
	}
	if len(raw) < 28 {
		return errShortBox("mvhd")
	}
	binary.BigEndian.PutUint32(raw[24:28], clampUint32(d))
	return nil
}

func clampUint32(v uint64) uint32 {
	if v > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(v)
}

// patchElstDuration 更新 edit list 第一条目的 segment_duration（movie timescale）。
//
// ⛔ 最容易漏、也最隐蔽的一处：漏了它，sample 表、帧数、mvhd/tkhd/mdhd 全都对，
// 但 ffmpeg / 播放器按 edit list 算出的**时长仍停在第一片** ——
// 文件能播、画面正常，只有进度条短了一截、后面拉不到。
// ZLM 录出的每个 trak 都带一条 elst（segment_duration = 整段时长）。
func patchElstDuration(raw []byte, d uint64) error {
	if len(raw) < 16 {
		return errShortBox("elst")
	}
	count := binary.BigEndian.Uint32(raw[12:16])
	switch {
	case count == 0:
		return nil
	case count > 1:
		// 多条目 elst 的语义是分段映射，拼起来不是"把第一条改大"就行。
		return fmt.Errorf("mp4join: elst 含 %d 个条目，暂不支持（交回多文件下载）", count)
	}
	if raw[8] == 1 {
		if len(raw) < 24 {
			return errShortBox("elst")
		}
		binary.BigEndian.PutUint64(raw[16:24], d)
		return nil
	}
	if len(raw) < 20 {
		return errShortBox("elst")
	}
	binary.BigEndian.PutUint32(raw[16:20], clampUint32(d))
	return nil
}
