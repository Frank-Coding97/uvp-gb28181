package directory

import (
	"errors"
	"strconv"
	"strings"
)

var ErrDirectoryFilterInvalid = errors.New("directory filter invalid")

type FilterKind string

const (
	FilterNationalArea    FilterKind = "national_area"
	FilterNationalUnknown FilterKind = "national_unknown"
	FilterNationalCatalog FilterKind = "national_catalog"
	FilterCustomGroup     FilterKind = "custom_group"
	FilterCustomUngrouped FilterKind = "custom_ungrouped"
)

type Filter struct {
	View            string
	Key             string
	Kind            FilterKind
	Code            string
	GroupID         uint
	NodeID          uint
	OwnerDeptID     uint
	OwnerDeptScoped bool
}

func ParseFilter(view, key, legacyNodeID string) (*Filter, error) {
	view, key, legacyNodeID = strings.TrimSpace(view), strings.TrimSpace(key), strings.TrimSpace(legacyNodeID)
	if legacyNodeID != "" {
		if view != "" || key != "" {
			return nil, ErrDirectoryFilterInvalid
		}
		id, err := strconv.ParseUint(legacyNodeID, 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		return &Filter{NodeID: uint(id)}, nil
	}
	if view == "" && key == "" {
		return nil, nil
	}
	if view == "" || key == "" {
		return nil, ErrDirectoryFilterInvalid
	}
	f := &Filter{View: view, Key: key}
	switch {
	case view == "national" && key == "national:unknown":
		f.Kind = FilterNationalUnknown
	case view == "national" && strings.HasPrefix(key, "national:area:"):
		f.Kind, f.Code = FilterNationalArea, strings.TrimPrefix(key, "national:area:")
		if !validDigits(f.Code) || (len(f.Code) != 6 && len(f.Code) != 8) {
			return nil, ErrDirectoryFilterInvalid
		}
	case view == "national" && strings.HasPrefix(key, "national:catalog:"):
		f.Kind = FilterNationalCatalog
		id, err := strconv.ParseUint(strings.TrimPrefix(key, "national:catalog:"), 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		f.NodeID = uint(id)
	case view == "custom" && key == "custom:ungrouped":
		f.Kind = FilterCustomUngrouped
	case view == "custom" && strings.HasPrefix(key, "custom:ungrouped:"):
		f.Kind = FilterCustomUngrouped
		id, err := strconv.ParseUint(strings.TrimPrefix(key, "custom:ungrouped:"), 10, 64)
		if err != nil {
			return nil, ErrDirectoryFilterInvalid
		}
		f.OwnerDeptID = uint(id)
		f.OwnerDeptScoped = true
	case view == "custom" && strings.HasPrefix(key, "custom:group:"):
		f.Kind = FilterCustomGroup
		id, err := strconv.ParseUint(strings.TrimPrefix(key, "custom:group:"), 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		f.GroupID = uint(id)
	default:
		return nil, ErrDirectoryFilterInvalid
	}
	return f, nil
}
