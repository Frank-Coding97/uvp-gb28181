<script setup lang="ts">
import { ref, onMounted } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    getZLMNodeConfig,
    updateZLMNodeConfig,
    testZLMNodeConnection,
    type ConfigGroup,
    type UpdateConfigResp
} from "@/api/gb28181-zlm";

const props = defineProps<{ nodeId: number }>();

const groups = ref<ConfigGroup[]>([]);
const loading = ref(false);
const saving = ref(false);
const testing = ref(false);
const dirty = ref<Record<string, string>>({});

async function refresh() {
    loading.value = true;
    try {
        const res = await getZLMNodeConfig(props.nodeId);
        if (res.code === 0) {
            groups.value = res.data.groups || [];
            dirty.value = {};
        }
    } catch (e: any) {
        Message.error(e?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

function onChange(key: string, value: string) {
    dirty.value[key] = value;
}

async function handleSave() {
    if (Object.keys(dirty.value).length === 0) {
        Message.info("没有改动");
        return;
    }
    saving.value = true;
    try {
        const res = await updateZLMNodeConfig(props.nodeId, dirty.value);
        if (res.code === 0) {
            const d: UpdateConfigResp = res.data;
            const msgs: string[] = [];
            if (d.applied.length) {
                msgs.push(`✅ 已生效: ${d.applied.join(", ")}`);
            }
            if (d.requiresRestart.length) {
                msgs.push(`⚠️ 需重启 ZLM 才生效: ${d.requiresRestart.join(", ")}`);
            }
            if (d.unknown.length) {
                msgs.push(`❓ 未知 key (已 fallback 下发): ${d.unknown.join(", ")}`);
            }
            Message.success(msgs.join("\n") || "已保存");
            refresh();
        }
    } catch (e: any) {
        Message.error(e?.message || "保存失败");
    } finally {
        saving.value = false;
    }
}

async function handleTest() {
    testing.value = true;
    try {
        const res = await testZLMNodeConnection(props.nodeId);
        if (res.code === 0) {
            if (res.data.online) {
                Message.success(`节点在线,http.port=${res.data.httpPort || "?"}`);
            } else {
                Message.error(`节点不可达: ${res.data.error || "未知"}`);
            }
        }
    } catch (e: any) {
        Message.error(e?.message || "探测失败");
    } finally {
        testing.value = false;
    }
}

onMounted(refresh);
</script>

<template>
    <div class="zlm-node-config">
        <a-space style="margin-bottom: 16px">
            <a-button :loading="testing" @click="handleTest">测试连通性</a-button>
            <a-button type="primary" :loading="saving" @click="handleSave">保存</a-button>
            <a-button @click="refresh">刷新</a-button>
            <a-tag v-if="Object.keys(dirty).length > 0" color="orange">
                {{ Object.keys(dirty).length }} 项待保存
            </a-tag>
        </a-space>

        <a-spin :loading="loading">
            <a-collapse :default-active-key="groups.map((g) => g.name)">
                <a-collapse-item v-for="g in groups" :key="g.name" :header="g.name">
                    <a-table :data="g.items" :pagination="false" row-key="key">
                        <template #columns>
                            <a-table-column title="键" data-index="key" :width="280" />
                            <a-table-column title="当前值">
                                <template #cell="{ record }">
                                    <a-input
                                        :model-value="dirty[record.key] ?? record.value"
                                        size="small"
                                        @input="(v: string) => onChange(record.key, v)"
                                    />
                                </template>
                            </a-table-column>
                            <a-table-column title="默认" data-index="default" :width="120" />
                            <a-table-column title="可热改" :width="100">
                                <template #cell="{ record }">
                                    <a-tag v-if="record.hotReloadable" color="green">热</a-tag>
                                    <a-tag v-else color="orange">需重启</a-tag>
                                </template>
                            </a-table-column>
                            <a-table-column title="说明" data-index="comment" />
                        </template>
                    </a-table>
                </a-collapse-item>
            </a-collapse>
        </a-spin>
    </div>
</template>

<style scoped>
.zlm-node-config {
    padding: 0 8px;
}
</style>
