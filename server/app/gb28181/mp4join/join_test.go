package mp4join

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/mp4join/mp4fixture"
)

// ---------- 测试用 MP4 构造 ----------
//
// 素材 builder 在 mp4join/mp4fixture 包 —— recordcache 的合并编排测试要用同一份，
// ⛔ 两处各写一份必然漂移（改了一处忘了另一处）。这里只留薄别名与薄包装，
// 让下面的用例读起来还是原来的样子。

const (
	testSampleSize = mp4fixture.SampleSize
	testTimescale  = mp4fixture.Timescale
	testDelta      = mp4fixture.Delta // ms/sample
	testFill       = mp4fixture.Fill
)

func concatBytes(parts ...[]byte) []byte { return mp4fixture.Concat(parts...) }

func mkRawBox(typ string, payload []byte) []byte { return mp4fixture.RawBox(typ, payload) }

// spec 描述要造的分片形状。
type spec struct {
	handlers  []string // 轨道类型，如 []string{"soun", "vide"}
	tag       string   // stsd 里的编码标记（不同值表示编码参数不同）
	samples   uint32   // 每条轨道的样本数
	chunks    []int64  // 每条轨道的 chunk 起始（空则自动按顺序排）
	syncEvery uint32   // >0 时生成 stss：每 N 个样本一个同步样本
}

// buildFragment 造一个 ftyp + mdat + moov 的 MP4。每条轨道 1 个 chunk。
func buildFragment(t *testing.T, s spec) []byte {
	t.Helper()
	return mp4fixture.Fragment(mp4fixture.Spec{
		Handlers:  s.handlers,
		Tag:       s.tag,
		Samples:   s.samples,
		Chunks:    s.chunks,
		SyncEvery: s.syncEvery,
	})
}

func writeFragment(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// readFragment 用生产代码把结果重新解析回来，便于断言。
func readFragment(t *testing.T, path string) *fragment {
	t.Helper()
	frag, err := scanFragment(path)
	require.NoError(t, err)
	return frag
}

// ---------- 主路径 ----------

func TestConcatMergesSampleTablesAndRebasesOffsets(t *testing.T) {
	dir := t.TempDir()
	a := writeFragment(t, dir, "a.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 10}))
	b := writeFragment(t, dir, "b.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 6}))
	out := filepath.Join(dir, "merged.mp4")

	require.NoError(t, Concat(out, []string{a, b}))

	meta := readFragment(t, out)
	require.Len(t, meta.tracks, 1)
	table := meta.tracks[0].table

	require.Equal(t, uint32(16), table.sampleCount, "样本数应是两片之和")
	require.Len(t, table.offsets, 2, "两片各一个 chunk")
	require.Len(t, table.stsc, 2, "chunk 序号偏移后两片的映射规则不能合并")

	// ⛔ 最关键的一条：chunk 偏移必须跟着 mdat 的新落点走。
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	for i, off := range table.offsets {
		require.GreaterOrEqual(t, off, meta.mdatDataStart, "第 %d 个 chunk 偏移在 mdat 之前", i)
		require.Less(t, off, meta.mdatDataStart+meta.mdatDataSize, "第 %d 个 chunk 偏移越界", i)
		require.Equal(t, byte(testFill), raw[off], "第 %d 个 chunk 的偏移没指向 mdat 数据", i)
	}
	require.Greater(t, table.offsets[1], table.offsets[0], "第二个分片的偏移必须更大")

	// 时长：16 个样本 × 40ms，mvhd/tkhd/mdhd 都要更新。
	wantDur := uint32(16 * testDelta)
	mvhd := childOf(meta.moov, "mvhd")
	require.Equal(t, wantDur, binary.BigEndian.Uint32(mvhd.raw[24:28]), "mvhd.duration")
	trak := childOf(meta.moov, "trak")
	tkhd := childOf(trak, "tkhd")
	require.Equal(t, wantDur, binary.BigEndian.Uint32(tkhd.raw[28:32]), "tkhd.duration")
	mdhd := childOf(childOf(trak, "mdia"), "mdhd")
	require.Equal(t, wantDur, binary.BigEndian.Uint32(mdhd.raw[24:28]), "mdhd.duration")
	// ⛔ edit list 决定播放器报出的呈现时长：只改前面几个字段、
	// 漏掉 elst，文件照样能播，但时长停在第一片。
	elst := childOf(childOf(trak, "edts"), "elst")
	require.NotNil(t, elst, "fixture 应带 elst（真实素材都有）")
	require.Equal(t, wantDur, binary.BigEndian.Uint32(elst.raw[16:20]), "elst.segment_duration")

	// stts 的 run 合并后仍是 16 个样本。
	var total uint32
	for _, e := range table.stts {
		total += e.count
	}
	require.Equal(t, uint32(16), total)
}

func TestConcatKeepsMediaBytesIntact(t *testing.T) {
	dir := t.TempDir()
	a := writeFragment(t, dir, "a.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 4}))
	b := writeFragment(t, dir, "b.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 8}))
	out := filepath.Join(dir, "merged.mp4")
	require.NoError(t, Concat(out, []string{a, b}))

	// 无损的硬约束：输出里的 mdat 数据区必须等于两片 mdat 数据区依次相连。
	wantMedia := bytes.Repeat([]byte{testFill}, 12*testSampleSize)
	got := readFragment(t, out)
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	gotMedia := raw[got.mdatDataStart : got.mdatDataStart+got.mdatDataSize]
	require.Equal(t, wantMedia, gotMedia)
	require.Equal(t, int64(len(wantMedia)), got.mdatDataSize, "输出只应有一个 mdat")
}

func TestConcatHandlesMultipleTracks(t *testing.T) {
	dir := t.TempDir()
	s := spec{handlers: []string{"soun", "vide"}, tag: "hvc1", samples: 5}
	a := writeFragment(t, dir, "a.mp4", buildFragment(t, s))
	b := writeFragment(t, dir, "b.mp4", buildFragment(t, s))
	out := filepath.Join(dir, "merged.mp4")

	require.NoError(t, Concat(out, []string{a, b}))

	meta := readFragment(t, out)
	require.Len(t, meta.tracks, 2)
	require.Equal(t, "soun", meta.tracks[0].handler)
	require.Equal(t, "vide", meta.tracks[1].handler)
	for i, track := range meta.tracks {
		require.Equal(t, uint32(10), track.table.sampleCount, "第 %d 条轨道样本数", i)
		require.Len(t, track.table.offsets, 2, "第 %d 条轨道 chunk 数", i)
	}

	// 每条轨道的 chunk 偏移都必须落在 mdat 数据区里。
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	for i, track := range meta.tracks {
		for j, off := range track.table.offsets {
			require.Equal(t, byte(testFill), raw[off], "轨道 %d 的第 %d 个 chunk 偏移错了", i, j)
		}
	}
}

func TestConcatSingleFragmentCopiesVerbatim(t *testing.T) {
	dir := t.TempDir()
	src := buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 3})
	path := writeFragment(t, dir, "only.mp4", src)
	out := filepath.Join(dir, "merged.mp4")

	require.NoError(t, Concat(out, []string{path}))
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, src, got, "单片必须原样复制（不做任何改造）")
}

func TestConcatRebasesSyncSampleNumbersAcrossFragments(t *testing.T) {
	dir := t.TempDir()
	shape := spec{handlers: []string{"vide"}, tag: "avc1", samples: 4, syncEvery: 2}
	parts := []string{
		writeFragment(t, dir, "1.mp4", buildFragment(t, shape)),
		writeFragment(t, dir, "2.mp4", buildFragment(t, shape)),
		writeFragment(t, dir, "3.mp4", buildFragment(t, shape)),
	}
	out := filepath.Join(dir, "merged.mp4")
	require.NoError(t, Concat(out, parts))

	meta := readFragment(t, out)
	table := meta.tracks[0].table
	require.Equal(t, uint32(12), table.sampleCount)
	require.Len(t, table.offsets, 3, "三片各一个 chunk")
	require.True(t, table.hasStss)

	// 每片各 4 个样本、每 2 个取一个同步样本 ⇒ 片内都是 [1, 3]。
	// ⛔ 跨片必须各自加上前面各片的样本数（0 / 4 / 8）。
	// 不加重排的话三片都写 [1,3]，播放器 seek 会跳到错误的画面 ——
	// 文件照样能播、时长也对，只有拖动进度条时才暴露。
	require.Equal(t, []uint32{1, 3, 5, 7, 9, 11}, table.stss)
}

// ---------- 拒绝路径 ----------

func TestConcatRejectsCodecMismatch(t *testing.T) {
	dir := t.TempDir()
	a := writeFragment(t, dir, "a.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 3}))
	b := writeFragment(t, dir, "b.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "hvc1", samples: 3}))
	out := filepath.Join(dir, "merged.mp4")

	err := Concat(out, []string{a, b})
	require.ErrorIs(t, err, ErrCodecMismatch, "编码参数不同必须拒绝，而不是拼出花屏文件")
	_, statErr := os.Stat(out)
	require.True(t, os.IsNotExist(statErr), "失败时不能留下半截输出")
}

func TestConcatRejectsTrackMismatch(t *testing.T) {
	dir := t.TempDir()
	a := writeFragment(t, dir, "a.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 3}))
	b := writeFragment(t, dir, "b.mp4", buildFragment(t, spec{handlers: []string{"soun", "vide"}, tag: "avc1", samples: 3}))
	out := filepath.Join(dir, "merged.mp4")

	require.ErrorIs(t, Concat(out, []string{a, b}), ErrTrackMismatch)
}

func TestConcatRejectsHandlerOrderMismatch(t *testing.T) {
	dir := t.TempDir()
	a := writeFragment(t, dir, "a.mp4", buildFragment(t, spec{handlers: []string{"soun", "vide"}, tag: "avc1", samples: 3}))
	b := writeFragment(t, dir, "b.mp4", buildFragment(t, spec{handlers: []string{"vide", "soun"}, tag: "avc1", samples: 3}))
	out := filepath.Join(dir, "merged.mp4")

	require.ErrorIs(t, Concat(out, []string{a, b}), ErrTrackMismatch)
}

func TestConcatRejectsFragmentedMP4(t *testing.T) {
	dir := t.TempDir()
	good := buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 3})
	bad := concatBytes(good, mkRawBox("moof", make([]byte, 16)))
	a := writeFragment(t, dir, "a.mp4", good)
	b := writeFragment(t, dir, "b.mp4", bad)
	out := filepath.Join(dir, "merged.mp4")

	require.ErrorIs(t, Concat(out, []string{a, b}), ErrNotRegularMP4)
}

func TestConcatRejectsEmptyAndMissingInput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "merged.mp4")

	require.Error(t, Concat(out, nil), "没有分片要报错，不能当成空文件成功")

	only := writeFragment(t, dir, "empty.mp4", nil)
	require.Error(t, Concat(out, []string{only}), "空分片要报错")

	require.Error(t, Concat(out, []string{filepath.Join(dir, "missing.mp4")}))
}

func TestConcatOverwritesExistingDestinationAtomically(t *testing.T) {
	dir := t.TempDir()
	a := writeFragment(t, dir, "a.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 3}))
	b := writeFragment(t, dir, "b.mp4", buildFragment(t, spec{handlers: []string{"vide"}, tag: "avc1", samples: 3}))
	out := writeFragment(t, dir, "merged.mp4", []byte("stale content"))

	require.NoError(t, Concat(out, []string{a, b}))
	meta := readFragment(t, out)
	require.Equal(t, uint32(6), meta.tracks[0].table.sampleCount)
	_, err := os.Stat(out + ".tmp")
	require.True(t, os.IsNotExist(err), "临时文件必须被 rename 掉，不能残留")
}

// ---------- 表解析 / 编码 ----------

func TestSampleTableRoundTrip(t *testing.T) {
	stts := []sttsEntry{{count: 3, delta: 40}, {count: 2, delta: 80}}
	got, err := parseStts(encodeStts(stts))
	require.NoError(t, err)
	require.Equal(t, stts, got)

	stsc := []stscEntry{{firstChunk: 1, perChunk: 5, descIndex: 1}, {firstChunk: 4, perChunk: 2, descIndex: 1}}
	gotSc, err := parseStsc(encodeStsc(stsc))
	require.NoError(t, err)
	require.Equal(t, stsc, gotSc)

	offsets := []int64{1024, 2048, 1 << 33}
	kind, raw := encodeChunkOffsets(offsets, false)
	require.Equal(t, "co64", kind, "超过 32 位范围必须自动升级成 co64")
	gotOff, err := parseCo64(raw)
	require.NoError(t, err)
	require.Equal(t, offsets, gotOff)

	fixed, count, sizes, err := parseStsz(encodeStsz(testSampleSize, nil, 7))
	require.NoError(t, err)
	require.Equal(t, uint32(testSampleSize), fixed)
	require.Equal(t, uint32(7), count)
	require.Nil(t, sizes)

	fixed, count, sizes, err = parseStsz(encodeStsz(0, []uint32{1, 2, 3}, 0))
	require.NoError(t, err)
	require.Zero(t, fixed)
	require.Equal(t, uint32(3), count)
	require.Equal(t, []uint32{1, 2, 3}, sizes)
}

func TestAppendSttsMergesAdjacentRuns(t *testing.T) {
	got := appendStts([]sttsEntry{{count: 2, delta: 40}}, []sttsEntry{{count: 3, delta: 40}, {count: 1, delta: 80}})
	require.Equal(t, []sttsEntry{{count: 5, delta: 40}, {count: 1, delta: 80}}, got)
	require.Equal(t, uint64(5*40+80), sttsDuration(got))
}

// ---------- 真实素材端到端 ----------

// TestConcatRealFragments 用 ZLM 真实录出的分片做端到端验证。
//
// ⛔ 上面那些用例只证明"逻辑自洽"，证明不了"真文件拼出来播放器认"。
// 这条用真实素材（HEVC 视频 + G.711 音频、moov 在尾、单片 ~84 秒）跑，
// 需要 MP4JOIN_REAL_DIR 指向放着 a.mp4 / b.mp4 的目录，默认跳过。
//
// 用法（在素材所在机器上）：
//
//	MP4JOIN_REAL_DIR=/tmp/frags go test -run TestConcatRealFragments -v ./...
func TestConcatRealFragments(t *testing.T) {
	dir := os.Getenv("MP4JOIN_REAL_DIR")
	if dir == "" {
		t.Skip("未设置 MP4JOIN_REAL_DIR，跳过真实素材验证")
	}
	a, b := filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4")
	out := filepath.Join(dir, "merged.mp4")

	require.NoError(t, Concat(out, []string{a, b}))

	// 合并结果必须能被自己重新解析，并且结构与素材一致。
	meta := readFragment(t, out)
	require.Len(t, meta.tracks, 2, "素材是音频 + 视频两条轨道")
	require.NotEmpty(t, meta.mdatDataSize)

	// 时长 ≈ 两片之和（素材 83.88s + 84.72s）。容差留够：音视频两轨的
	// 分片边界本来就不同步，各自的时长会有几百毫秒差。
	sumA, sumB := realDurationMS(t, a), realDurationMS(t, b)
	for i, tr := range meta.tracks {
		got := float64(sttsDuration(tr.table.stts)) / float64(tr.timescale) * 1000
		require.InDelta(t, sumA+sumB, got, 2000,
			"第 %d 条轨道合并后时长应与两片之和相当（%.0fms）", i, sumA+sumB)
	}

	// 输出体积 = ftyp + mdat 头 + 两片数据区 + 新 moov。
	info, err := os.Stat(out)
	require.NoError(t, err)
	require.Greater(t, info.Size(), fileSize(t, a)+fileSize(t, b)-(1<<20),
		"合并后体积应与两片之和相当（只多一个 moov、少一个 ftyp 头）")
}

func realDurationMS(t *testing.T, path string) float64 {
	t.Helper()
	meta, err := scanFragment(path)
	require.NoError(t, err)
	var maxMS float64
	for _, tr := range meta.tracks {
		ms := float64(sttsDuration(tr.table.stts)) / float64(tr.timescale) * 1000
		if ms > maxMS {
			maxMS = ms
		}
	}
	return maxMS
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Size()
}
