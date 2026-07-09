<template>
    <div>
        <div class="login_form_box">
            <a-form :rules="rules" :model="form" layout="vertical" @submit="onSubmit">
                <a-form-item field="tenantCode" :hide-asterisk="true">
                    <a-input v-model="form.tenantCode" allow-clear placeholder="租户编码（不填则为账号默认租户）">
                        <template #prefix>
                            <icon-home />
                        </template>
                    </a-input>
                </a-form-item>
                <a-form-item field="username" :hide-asterisk="true">
                    <a-input v-model="form.username" allow-clear placeholder="请输入账号">
                        <template #prefix>
                            <icon-user />
                        </template>
                    </a-input>
                </a-form-item>
                <a-form-item field="password" :hide-asterisk="true">
                    <a-input-password v-model="form.password" allow-clear placeholder="请输入密码">
                        <template #prefix>
                            <icon-lock />
                        </template>
                    </a-input-password>
                </a-form-item>
                <a-form-item v-if="isCaptchaEnabled" field="captchaValue" :hide-asterisk="true">
                    <div class="verifyCode">
                        <a-input class="verifyCodeInput" v-model="form.captchaValue" allow-clear placeholder="请输入验证码" />
                        <!-- <s-verify-code :content-height="30" :font-size-max="30" :content-width="110"
                            @verify-code-change="verifyCodeChange" /> -->
                        <img :src="captchaImgUrl" class="verifyCodeImg"
                            @click="refreshCaptcha" />
                    </div>
                </a-form-item>
                <!-- <a-form-item field="remember">
                    <div class="remember">
                        <a-checkbox v-model="form.remember">记住密码</a-checkbox>
                        <div class="forgot-password">忘记密码</div>
                    </div>
                </a-form-item> -->
                <a-form-item>
                    <a-button long type="primary" html-type="submit" :loading="loginLoading">登录</a-button>
                </a-form-item>
            </a-form>
        </div>
        <!-- <div class="register">注册账号</div> -->
    </div>
</template>

<script setup lang="ts">
import { useRouter } from "vue-router";
import { useRouteConfigStore } from "@/store/modules/route-config";
import { useUserStoreHook } from "@/store/modules/user";
import { computed, onMounted, ref, watch } from "vue";
import { getVerifyImgString } from "@/api/user";
import { useSystemStore } from "@/store/modules/system";
import { useSysConfigStore } from "@/store/modules/sys-config";

import { storeToRefs } from "pinia";
// 获取系统配置
const sysConfigStore = useSysConfigStore();
const { systemConfig, captchaConfig } = storeToRefs(sysConfigStore);
// 定义表单数据类型
interface LoginForm {
    tenantCode: string;
    username: string;
    password: string;
    captchaValue: string | null;
    captchaId: string;
}

// Store 和 Router
const routeStore = useRouteConfigStore();
const router = useRouter();

// 响应式数据
const loginLoading = ref(false);
const form = ref<LoginForm>({
    tenantCode: "",
    username: "",
    password: "",
    captchaValue: null,
    captchaId: ""
});


const isCaptchaEnabled = computed(() => captchaConfig.value.open);

// 表单验证规则
const rules = computed(() => {
    const baseRules: Record<string, Array<{ required: boolean; message: string }>> = {
        username: [
            {
                required: true,
                message: "请输入账号"
            }
        ],
        password: [
            {
                required: true,
                message: "请输入密码"
            }
        ]
    };

    if (isCaptchaEnabled.value) {
        baseRules.captchaValue = [
            {
                required: true,
                message: "请输入验证码"
            }
        ];
    }

    return baseRules;
});

// 提交表单
const onSubmit = async ({ errors }: { errors: Record<string, any> | undefined }) => {
    if (errors) return;
    await onLogin();
};

// 登录处理
const onLogin = async () => {
    try {
        // 新的登录逻辑
        loginLoading.value = true;

        // 执行登录
        const loginData = {
            ...form.value,
            captchaId: isCaptchaEnabled.value ? form.value.captchaId : "",
            captchaValue: isCaptchaEnabled.value ? form.value.captchaValue : null
        };

        await useUserStoreHook().loginByUsername(loginData);

        // 加载用户信息
        await useUserStoreHook().getUserInfo();

        // 加载路由信息
        await routeStore.initSetRouter();
        loginLoading.value = false;

        arcoMessage("success", "登录成功");

        // 跳转首页
        router.replace("/home");

        // 设置字典
        useSystemStore().setDictData();

    } catch (error) {
        console.error("登录失败:", error);
        //arcoMessage("error", typeof error === "string" ? error : "登录失败，请检查用户名和密码");
        form.value.captchaId = "";
        refreshCaptcha();
    } finally {
        loginLoading.value = false;
    }
};

// 验证码
const captchaImgUrl = ref("");
const refreshCaptcha = () => {
    if (!isCaptchaEnabled.value) {
        form.value.captchaId = "";
        form.value.captchaValue = null;
        captchaImgUrl.value = "";
        return;
    }
    getVerifyImgString().then(res => {
        form.value.captchaId = res.data.captchaId;
        captchaImgUrl.value = res.data.image;
    }).catch(err => {
        console.error("获取验证码失败:", err);
    });
};

// 监听系统配置变化，自动更新默认账号密码
watch(systemConfig, (newConfig) => {
    if (newConfig) {
        if (newConfig.defaultusername) {
            form.value.username = newConfig.defaultusername;
        }
        if (newConfig.defaultpassword) {
            form.value.password = newConfig.defaultpassword;
        }
    }
}, { immediate: true });

// 组件挂载时的初始化
onMounted(async () => {
    await sysConfigStore.getConfig().catch((error: unknown) => {
        console.warn("获取系统配置失败，将使用已缓存配置:", error);
    });
    refreshCaptcha();
});
</script>

<style lang="scss" scoped>
.login_form_box {
    margin-top: 28px;

    :deep(.arco-form-item) {
        margin-bottom: 20px;
    }

    :deep(.arco-form-item:last-child) {
        margin-bottom: 0;
    }

    :deep(.arco-input-wrapper) {
        height: 46px;
        padding: 0 14px;
        border: 1px solid #dbe6f4;
        border-radius: 12px;
        background: linear-gradient(180deg, #f8fbff 0%, #f3f7fc 100%);
        box-shadow:
            inset 0 1px 0 rgba(255, 255, 255, .9),
            0 1px 2px rgba(15, 23, 42, .04);
        transition:
            border-color .18s ease,
            background .18s ease,
            box-shadow .18s ease,
            transform .18s ease;
    }

    :deep(.arco-input-wrapper:hover) {
        border-color: #b8cce7;
        background: #f7fbff;
        box-shadow:
            inset 0 1px 0 rgba(255, 255, 255, .95),
            0 4px 12px rgba(24, 144, 255, .08);
    }

    :deep(.arco-input-wrapper.arco-input-focus) {
        border-color: #1890ff;
        background: #fff;
        box-shadow:
            0 0 0 3px rgba(24, 144, 255, .12),
            0 8px 22px rgba(24, 144, 255, .12);
        transform: translateY(-1px);
    }

    :deep(.arco-input),
    :deep(.arco-input-password) {
        font-size: 14px;
        color: #1f2d3d;
    }

    :deep(.arco-input::placeholder) {
        color: #9aa8ba;
    }

    :deep(.arco-input-prefix),
    :deep(.arco-input-suffix),
    :deep(.arco-input-clear-btn),
    :deep(.arco-input-password-visibility-btn) {
        color: #7d8da1;
    }

    :deep(.arco-input-wrapper.arco-input-focus .arco-input-prefix),
    :deep(.arco-input-wrapper:hover .arco-input-prefix) {
        color: #1890ff;
    }

    .verifyCode {
        display: flex;
        align-items: center;
        gap: 10px;
        width: 100%;
    }

    .verifyCodeInput {
        flex: 1;
        min-width: 0;
    }

    .remember {
        display: flex;
        align-items: center;
        justify-content: space-between;
        width: 100%;

        .forgot-password {
            color: $color-primary;
            cursor: pointer;
        }
    }
}

.register {
    font-size: $font-size-body-1;
    color: $color-text-3;
    text-align: center;
    cursor: pointer;
}

.verifyCodeImg {
    cursor: pointer;
    height: 46px;
    width: 142px;
    flex: 0 0 142px;
    border: 1px solid #dbe6f4;
    border-radius: 12px;
    background: #f8fbff;
    object-fit: cover;
    transition:
        border-color .18s ease,
        box-shadow .18s ease,
        transform .18s ease;
}

.verifyCodeImg:hover {
    border-color: #1890ff;
    box-shadow: 0 6px 18px rgba(24, 144, 255, .14);
    transform: translateY(-1px);
}
</style>
