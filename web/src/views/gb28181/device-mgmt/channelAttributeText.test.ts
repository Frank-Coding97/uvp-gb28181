import { describe, expect, it } from "vitest";
import {
    CHANNEL_ATTR_UNREPORTED,
    catalogShapeFromAttributes,
    catalogShapeText,
    channelAttributeEntries,
    channelEndpointText,
    directionTypeText,
    photoelectricImagingTypeText,
    positionTypeText,
    roomTypeText,
    supplyLightTypeText,
    useTypeText
} from "./channelAttributeText";

/**
 * 判定依据:GB/T 28181 附录 A —— 2016 与 2022 两版 `<Info>` 的值域差异。
 * 这一组的核心防回归点是「0 / '' 一律按未上报,不能当值是 0 展示」。
 */
describe("channelAttributeText 枚举映射", () => {
    it.each([
        [1, "室外"],
        [2, "室内"]
    ])("RoomType %i → %s(两版编码一致)", (value, expected) => {
        expect(roomTypeText(value)).toBe(expected);
    });

    it.each([
        [1, "无补光"],
        [2, "红外补光"],
        [3, "白光补光"],
        // 4 / 9 是 2022 新增,2016 只有 1-3。
        [4, "激光补光"],
        [9, "其他"]
    ])("SupplyLightType %i → %s", (value, expected) => {
        expect(supplyLightTypeText(value)).toBe(expected);
    });

    it.each([
        [1, "东"],
        [2, "西"],
        [3, "南"],
        [4, "北"],
        [5, "东南"],
        [6, "东北"],
        [7, "西南"],
        [8, "西北"]
    ])("DirectionType %i → %s(两版编码一致)", (value, expected) => {
        expect(directionTypeText(value)).toBe(expected);
    });

    it.each([
        [1, "省际检查站"],
        [8, "校园周边"],
        [9, "治安复杂区域"],
        [10, "交通干线"]
    ])("PositionType %i → %s(2016 独有)", (value, expected) => {
        expect(positionTypeText(value)).toBe(expected);
    });

    it.each([
        [1, "治安"],
        [2, "交通"],
        [3, "重点"]
    ])("UseType %i → %s(2016 独有)", (value, expected) => {
        expect(useTypeText(value)).toBe(expected);
    });
});

describe("channelAttributeText 未上报语义", () => {
    it("把 0 / null / undefined / 空串一律视为未上报,而不是值为 0", () => {
        expect(roomTypeText(0)).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(roomTypeText(null)).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(roomTypeText(undefined)).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(supplyLightTypeText(0)).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(directionTypeText(0)).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(positionTypeText(0)).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(useTypeText(0)).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(photoelectricImagingTypeText("")).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(photoelectricImagingTypeText(null)).toBe(CHANNEL_ATTR_UNREPORTED);
    });

    it("值域外的编码照样回显原值,排障时不能被吞掉", () => {
        expect(roomTypeText(3)).toBe("未知(3)");
        expect(supplyLightTypeText(7)).toBe("未知(7)");
        expect(directionTypeText(9)).toBe("未知(9)");
    });
});

describe("photoelectricImagingTypeText 多值支持", () => {
    it("按标准要求用英文半角 / 分隔,逐段翻译后原样拼回", () => {
        expect(photoelectricImagingTypeText("1")).toBe("可见光成像");
        expect(photoelectricImagingTypeText("1/2")).toBe("可见光成像 / 热成像");
        expect(photoelectricImagingTypeText(" 1 / 2 / 9 ")).toBe("可见光成像 / 热成像 / 其他");
    });

    it("段内出现值域外编码时保留原值", () => {
        expect(photoelectricImagingTypeText("1/7")).toBe("可见光成像 / 未知(7)");
    });
});

describe("channelEndpointText", () => {
    it("IP 与端口都有值", () => {
        expect(channelEndpointText("192.168.1.64", 5060)).toBe("192.168.1.64:5060");
    });

    it("端口为 0 按未上报处理,只展示 IP", () => {
        expect(channelEndpointText("192.168.1.64", 0)).toBe("192.168.1.64");
    });

    it("只有端口时展示 :port,不编造 IP", () => {
        expect(channelEndpointText("", 5060)).toBe(":5060");
    });

    it("两者都缺按未上报", () => {
        expect(channelEndpointText("", 0)).toBe(CHANNEL_ATTR_UNREPORTED);
        expect(channelEndpointText(null, null)).toBe(CHANNEL_ATTR_UNREPORTED);
    });
});

describe("channelAttributeEntries", () => {
    it("2022 设备:共有项与 2022 独有项有值,2016 独有项未上报", () => {
        const entries = channelAttributeEntries({
            roomType: 2,
            supplyLightType: 4,
            directionType: 5,
            resolution: "1920*1080",
            ipAddress: "10.0.0.9",
            port: 5060,
            positionType: 0,
            useType: 0,
            photoelectricImagingType: "1",
            capturePositionType: "2"
        });
        const byKey = Object.fromEntries(entries.map(entry => [entry.key, entry]));

        expect(byKey.roomType).toMatchObject({ value: "室内", reported: true, scope: "both" });
        expect(byKey.supplyLightType).toMatchObject({ value: "激光补光", reported: true, scope: "both" });
        expect(byKey.directionType).toMatchObject({ value: "东南", reported: true, scope: "both" });
        expect(byKey.resolution).toMatchObject({ value: "1920*1080", reported: true });
        expect(byKey.endpoint).toMatchObject({ value: "10.0.0.9:5060", reported: true });

        expect(byKey.positionType).toMatchObject({ value: CHANNEL_ATTR_UNREPORTED, reported: false, scope: "2016" });
        expect(byKey.useType).toMatchObject({ value: CHANNEL_ATTR_UNREPORTED, reported: false, scope: "2016" });
        expect(byKey.photoelectricImagingType).toMatchObject({ value: "可见光成像", reported: true, scope: "2022" });
        expect(byKey.capturePositionType).toMatchObject({ value: "2", reported: true, scope: "2022" });
    });

    it("2016 设备:2016 独有项有值,2022 独有项未上报", () => {
        const entries = channelAttributeEntries({
            roomType: 1,
            supplyLightType: 2,
            directionType: 3,
            resolution: "1280*720",
            positionType: 6,
            useType: 1
        });
        const byKey = Object.fromEntries(entries.map(entry => [entry.key, entry]));

        expect(byKey.positionType).toMatchObject({ value: "商业中心", reported: true, scope: "2016" });
        expect(byKey.useType).toMatchObject({ value: "治安", reported: true, scope: "2016" });
        expect(byKey.photoelectricImagingType).toMatchObject({ value: CHANNEL_ATTR_UNREPORTED, reported: false, scope: "2022" });
        expect(byKey.capturePositionType).toMatchObject({ value: CHANNEL_ATTR_UNREPORTED, reported: false, scope: "2022" });
        // 2016 的补光方式只到 3,不该出现 2022 才有的 4/9。
        expect(byKey.supplyLightType).toMatchObject({ value: "红外补光", reported: true });
    });

    it("两版共有项固定排在前、版本独有项排在后", () => {
        const keys = channelAttributeEntries({}).map(entry => entry.key);
        expect(keys).toEqual([
            "roomType",
            "supplyLightType",
            "directionType",
            "resolution",
            "endpoint",
            "positionType",
            "useType",
            "photoelectricImagingType",
            "capturePositionType"
        ]);
    });

    it("属性全空的通道不抛异常,全部按未上报返回", () => {
        const entries = channelAttributeEntries({});
        expect(entries).toHaveLength(9);
        expect(entries.every(entry => entry.reported === false)).toBe(true);
        expect(entries.every(entry => entry.value === CHANNEL_ATTR_UNREPORTED)).toBe(true);
    });
});

describe("catalogShapeFromAttributes", () => {
    it("只有 2016 独有属性 → 2016 形态", () => {
        expect(catalogShapeFromAttributes({ positionType: 1 })).toBe("2016");
        expect(catalogShapeFromAttributes({ useType: 2 })).toBe("2016");
    });

    it("只有 2022 独有属性 → 2022 形态", () => {
        expect(catalogShapeFromAttributes({ photoelectricImagingType: "1" })).toBe("2022");
        expect(catalogShapeFromAttributes({ capturePositionType: "3" })).toBe("2022");
    });

    it("两组都有 → 两版混发(非合规设备)", () => {
        expect(catalogShapeFromAttributes({ positionType: 1, photoelectricImagingType: "1" })).toBe("both");
    });

    it("两组都没有 → 无法判断", () => {
        expect(catalogShapeFromAttributes({})).toBe("unknown");
        expect(catalogShapeFromAttributes({ positionType: 0, photoelectricImagingType: "" })).toBe("unknown");
    });

    it("空白字符串不算上报", () => {
        expect(catalogShapeFromAttributes({ photoelectricImagingType: "   " })).toBe("unknown");
    });

    it("给出可读文案", () => {
        expect(catalogShapeText("2016")).toBe("GB/T 28181-2016 形态");
        expect(catalogShapeText("2022")).toBe("GB/T 28181-2022 形态");
        expect(catalogShapeText("both")).toBe("两版属性混发");
        expect(catalogShapeText("unknown")).toBe("未上报版本独有属性");
    });
});
