import { config } from "@vue/test-utils";

config.global.stubs = {
    "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
    "a-input": { template: "<input />" },
    "a-input-password": { template: "<input type='password' />" },
    "a-input-number": { template: "<input type='number' />" },
    "a-modal": { template: "<div><slot name='title' /><slot /></div>" },
    "a-select": { template: "<select><slot /></select>" },
    "a-option": { template: "<option><slot /></option>" },
    "a-tag": { template: "<span><slot /></span>" }
};
