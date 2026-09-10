import { config } from "@vue/test-utils";

config.global.stubs = {
    "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
    "a-input": { template: "<input />" },
    "a-input-password": { template: "<input type='password' />" },
    "a-input-number": { template: "<input type='number' />" },
    "a-modal": { template: "<div><slot name='title' /><slot /></div>" },
    "a-select": { template: "<select><slot /></select>" },
    "a-option": { template: "<option><slot /></option>" },
    "a-tag": { template: "<span><slot /></span>" },
    "a-alert": { template: "<div class='a-alert'><slot /></div>" },
    "a-empty": { template: "<div class='a-empty'><slot /></div>" },
    "a-spin": { template: "<div class='a-spin'><slot /></div>" },
    "a-textarea": { template: "<textarea><slot /></textarea>" },
    "a-form-item": { template: "<div class='a-form-item'><slot /></div>" },
    "a-range-picker": { template: "<div class='a-range-picker'><slot /></div>" },
    "s-layout-search": { template: "<div class='s-layout-search'><slot name='fields' /><slot name='actions' /></div>" }
};
