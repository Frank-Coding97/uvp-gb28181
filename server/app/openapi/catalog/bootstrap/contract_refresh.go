package bootstrap

import (
	"context"
	"encoding/json"
	"strings"

	"gorm.io/gorm"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	catalogstore "uvplatform.com/uvp-gb28181/app/openapi/catalog/store"
	models "uvplatform.com/uvp-gb28181/app/openapi/models"
)

func sameSysAPIID(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
func sameSchema(left, right string) bool {
	if strings.TrimSpace(left) == strings.TrimSpace(right) {
		return true
	}
	var l, r any
	if json.Unmarshal([]byte(left), &l) != nil || json.Unmarshal([]byte(right), &r) != nil {
		return false
	}
	lb, _ := json.Marshal(l)
	rb, _ := json.Marshal(r)
	return string(lb) == string(rb)
}

// These fields describe the fixed executable contract. User names,
// descriptions, sorting, group assignments and enablement stay untouched.
func capabilityContractUpdates(row models.Capability, definition readDefinition, api *appmodels.SysApi) map[string]any {
	updates := map[string]any{}
	if row.SysAPIPath != definition.internalPath {
		updates["sys_api_path"] = definition.internalPath
	}
	if row.SysAPIMethod != definition.sysAPIMethod {
		updates["sys_api_method"] = definition.sysAPIMethod
	}
	if !sameSysAPIID(row.SysAPIID, sysAPIID(api)) {
		updates["sys_api_id"] = sysAPIID(api)
	}
	return updates
}
func operationContractUpdates(row models.Operation, definition readDefinition, api *appmodels.SysApi) map[string]any {
	updates := map[string]any{}
	if row.SysAPIPath != definition.internalPath {
		updates["sys_api_path"] = definition.internalPath
	}
	if row.SysAPIMethod != definition.sysAPIMethod {
		updates["sys_api_method"] = definition.sysAPIMethod
	}
	if !sameSysAPIID(row.SysAPIID, sysAPIID(api)) {
		updates["sys_api_id"] = sysAPIID(api)
	}
	if !sameSchema(row.RequestSchema, definition.requestSchema) {
		updates["request_schema"] = definition.requestSchema
	}
	if !sameSchema(row.ResponseSchema, definition.responseSchema) {
		updates["response_schema"] = definition.responseSchema
	}
	return updates
}

// Source drift needs an in-place repair; snapshot drift additionally needs a
// new immutable release. Compare schemas separately because runtime Draft
// intentionally does not carry the documentation schema fields.
func coreContractDrift(ctx context.Context, db *gorm.DB, active *catalogstore.ReleaseSnapshot) (sourceDrift, snapshotDrift bool, err error) {
	lookup, err := loadSeedLookup(ctx, db)
	if err != nil {
		return false, false, err
	}
	items := map[string]models.ReleaseItem{}
	if active != nil {
		for _, item := range active.Items {
			items[item.Scope] = item
		}
	}
	for _, definition := range coreCatalogDefinitions() {
		var api *appmodels.SysApi
		if row, found := lookup.apis[routeSeedKey{definition.sysAPIMethod, definition.internalPath}]; found {
			copy := row
			api = &copy
		}
		if capability, found := lookup.capabilities[definition.scope]; found && capability.DeletedAt == nil {
			if len(capabilityContractUpdates(capability, definition, api)) != 0 {
				sourceDrift = true
			}
			if operation, found := lookup.operations[operationSeedKey{capability.ID, definition.code}]; found && operation.DeletedAt == nil && len(operationContractUpdates(operation, definition, api)) != 0 {
				sourceDrift = true
			}
		}
		if item, found := items[definition.scope]; found {
			if item.SysAPIPath != definition.internalPath || item.SysAPIMethod != definition.sysAPIMethod || !sameSysAPIID(item.SysAPIID, sysAPIID(api)) || !sameSchema(item.RequestSchema, definition.requestSchema) || !sameSchema(item.ResponseSchema, definition.responseSchema) {
				snapshotDrift = true
			}
		}
	}
	return sourceDrift, snapshotDrift, nil
}
