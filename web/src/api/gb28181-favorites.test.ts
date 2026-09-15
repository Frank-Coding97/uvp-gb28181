import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { appendChannelFavoriteGroup, createChannelFavoriteGroup, deleteChannelFavoriteGroup, listChannelFavoriteGroups, removeChannelFavoriteItem } from "./gb28181";

describe("通道收藏 API", () => {
	beforeEach(() => { request.mockReset(); request.mockResolvedValue({ code: 0, data: {} }); });
	it("使用受保护的收藏组路径和用户态请求", async () => {
		await listChannelFavoriteGroups();
		expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/channel-favorite-groups");
		await createChannelFavoriteGroup("重点", [{ deviceCode: "D1", channelCode: "C1" }]);
		expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/channel-favorite-groups", { data: { name: "重点", channels: [{ deviceCode: "D1", channelCode: "C1" }] } });
		await appendChannelFavoriteGroup(3, [{ deviceCode: "D1", channelCode: "C2" }]);
		expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/channel-favorite-groups/3/channels", { data: { channels: [{ deviceCode: "D1", channelCode: "C2" }] } });
		await removeChannelFavoriteItem(3, { deviceCode: "D1", channelCode: "C2" });
		expect(request).toHaveBeenLastCalledWith("delete", "/api/gb28181/channel-favorite-groups/3/channels", { data: { deviceCode: "D1", channelCode: "C2" } });
		await deleteChannelFavoriteGroup(3);
		expect(request).toHaveBeenLastCalledWith("delete", "/api/gb28181/channel-favorite-groups/3");
	});
});
