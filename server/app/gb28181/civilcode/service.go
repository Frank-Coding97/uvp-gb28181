package civilcode

import (
	"context"
	"strings"
	"sync"

	"gorm.io/gorm"
)

// Service 行政区划字典查询服务(进程内缓存)
// WarmCache 启动期填充 sync.Map,后续 Lookup/Children/Search 全部 O(1)/O(n) 走缓存
type Service struct {
	db         *gorm.DB
	byCode     sync.Map // code -> *SysCivilCode
	byParent   sync.Map // parent_code -> []*SysCivilCode
	all        []*SysCivilCode
	allMu      sync.RWMutex
	warmedOnce sync.Once
}

// NewService 构造 Service(不自动 warm,调用方在 bootstrap 显式 WarmCache)
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// WarmCache 启动时一次性载入全表到进程内 map
// 数据量 ~3500 行,~400KB,内存代价极小
func (s *Service) WarmCache(ctx context.Context) error {
	var err error
	s.warmedOnce.Do(func() {
		var rows []SysCivilCode
		if e := s.db.WithContext(ctx).Find(&rows).Error; e != nil {
			err = e
			return
		}
		parentMap := make(map[string][]*SysCivilCode, len(rows))
		all := make([]*SysCivilCode, 0, len(rows))
		for i := range rows {
			r := &rows[i]
			s.byCode.Store(r.Code, r)
			parentMap[r.ParentCode] = append(parentMap[r.ParentCode], r)
			all = append(all, r)
		}
		for pc, list := range parentMap {
			s.byParent.Store(pc, list)
		}
		s.allMu.Lock()
		s.all = all
		s.allMu.Unlock()
	})
	return err
}

// Lookup 按 6 位行政区码查名称(命中缓存,O(1))
// 未命中返回 nil(调用方判空)
func (s *Service) Lookup(code string) *SysCivilCode {
	if v, ok := s.byCode.Load(code); ok {
		return v.(*SysCivilCode)
	}
	return nil
}

// Children 返回某行政区码的直接下级
// 例:Children("370100") → 济南各区/县
func (s *Service) Children(parentCode string) []*SysCivilCode {
	if v, ok := s.byParent.Load(parentCode); ok {
		// 返回拷贝防外部 mutate
		raw := v.([]*SysCivilCode)
		out := make([]*SysCivilCode, len(raw))
		copy(out, raw)
		return out
	}
	return nil
}

// SearchByName 关键词命中名称(前缀+包含,最多返回 limit 条)
// 拼音搜索 Phase 2 加(本期 pinyin 字段为空)
func (s *Service) SearchByName(keyword string, limit int) []*SysCivilCode {
	if keyword == "" {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	kw := strings.ToLower(keyword)
	s.allMu.RLock()
	defer s.allMu.RUnlock()
	out := make([]*SysCivilCode, 0, limit)
	for _, r := range s.all {
		if strings.Contains(strings.ToLower(r.Name), kw) || strings.Contains(strings.ToLower(r.ShortName), kw) {
			out = append(out, r)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// AllCount 缓存中记录数(给监控/测试用)
func (s *Service) AllCount() int {
	s.allMu.RLock()
	defer s.allMu.RUnlock()
	return len(s.all)
}
