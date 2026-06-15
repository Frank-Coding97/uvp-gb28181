import SparkMD5 from "spark-md5"; // 导入 SparkMD5 库，用于计算文件的 MD5 哈希值
import { chunkInitAPI, chunkUploadAPI, chunkMergeAPI, chunkCancelAPI } from "@/api/file"; // 导入分片上传相关的 API 接口
import type { ChunkInitParams, ChunkMergeParams } from "@/api/file"; // 导入分片上传参数的类型定义

// ===== 类型定义 =====
// 定义分片上传的状态类型，包含空闲、计算哈希中、上传中、已暂停、合并中、完成、错误七种状态
export type ChunkUploadStatus = "idle" | "hashing" | "uploading" | "paused" | "merging" | "done" | "error";

// 定义分片上传进度信息的接口
export interface ChunkProgress {
    status: ChunkUploadStatus; // 当前上传状态
    totalChunks: number; // 总分片数量
    uploadedChunks: number; // 已上传的分片数量
    percent: number; // 上传进度百分比（0-100）
    currentSpeed: string; // 当前上传速度，格式为 "XX KB/s"
    remainingTime: string; // 预计剩余时间，格式为 "HH:MM:SS" 或 "MM:SS"
    hashingPercent: number; // MD5 哈希计算的进度百分比（0-100）
}

// 定义分片上传配置的接口
export interface ChunkUploadConfig {
    chunkSize: number; // 每个分片的大小（单位：字节）
    concurrency: number; // 并发上传的分片数量
    maxRetry: number; // 分片上传失败时的最大重试次数
    largeFileThreshold: number; // 大文件阈值（单位：字节），文件大小超过此值时使用分片上传
    maxFileSize: number; // 最大文件大小（单位：字节）
}

// 定义上传回调函数的接口
export interface UploadCallbacks {
    onProgress: (progress: ChunkProgress) => void; // 进度更新回调函数
    onSuccess: (result: any) => void; // 上传成功回调函数
    onError: (error: Error) => void; // 上传失败回调函数
}

// ===== 配置读取 =====
// 定义默认的分片上传配置
const DEFAULT_CONFIG: ChunkUploadConfig = {
    chunkSize: 5 * 1024 * 1024, // 默认分片大小为 5MB
    concurrency: 3, // 默认并发上传数量为 3
    maxRetry: 3, // 默认最大重试次数为 3
    largeFileThreshold: 50 * 1024 * 1024, // 默认大文件阈值为 50MB
    maxFileSize: 5 * 1024 * 1024 * 1024 // 默认最大文件大小为 5GB
};

// 获取分片上传配置的函数，支持通过参数或环境变量覆盖默认配置
export function getChunkUploadConfig(overrides?: Partial<ChunkUploadConfig>): ChunkUploadConfig {
    return {
        // 分片大小：优先使用传入的配置，其次使用环境变量，最后使用默认值
        chunkSize: overrides?.chunkSize
            || Number(import.meta.env.VITE_CHUNK_SIZE)
            || DEFAULT_CONFIG.chunkSize,
        // 并发数量：优先使用传入的配置，其次使用环境变量，最后使用默认值
        concurrency: overrides?.concurrency
            || Number(import.meta.env.VITE_CHUNK_CONCURRENCY)
            || DEFAULT_CONFIG.concurrency,
        // 最大重试次数：优先使用传入的配置，其次使用环境变量，最后使用默认值
        maxRetry: overrides?.maxRetry
            || Number(import.meta.env.VITE_CHUNK_MAX_RETRY)
            || DEFAULT_CONFIG.maxRetry,
        // 大文件阈值：优先使用传入的配置，其次使用环境变量，最后使用默认值
        largeFileThreshold: overrides?.largeFileThreshold
            || Number(import.meta.env.VITE_CHUNK_LARGE_FILE_THRESHOLD)
            || DEFAULT_CONFIG.largeFileThreshold,
        // 最大文件大小：优先使用传入的配置，其次使用环境变量，最后使用默认值
        maxFileSize: overrides?.maxFileSize
            || Number(import.meta.env.VITE_CHUNK_MAX_FILE_SIZE)
            || DEFAULT_CONFIG.maxFileSize
    };
}

// ===== 工具函数 =====
// 格式化上传速度的函数，将字节/秒转换为易读的单位（B/s、KB/s、MB/s、GB/s）
function formatSpeed(bytesPerSecond: number): string {
    if (bytesPerSecond <= 0) return "0 KB/s"; // 如果速度小于等于0，返回 "0 KB/s"
    const k = 1024; // 定义单位换算基数
    const units = ["B/s", "KB/s", "MB/s", "GB/s"]; // 定义速度单位数组
    const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k)); // 计算应该使用的单位索引
    return parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(1)) + " " + units[i]; // 返回格式化后的速度字符串
}

// 格式化剩余时间的函数，将秒数转换为 "HH:MM:SS" 或 "MM:SS" 格式
function formatRemainingTime(seconds: number): string {
    if (!isFinite(seconds) || seconds <= 0) return "--:--"; // 如果时间无效或小于等于0，返回 "--:--"
    const h = Math.floor(seconds / 3600); // 计算小时数
    const m = Math.floor((seconds % 3600) / 60); // 计算分钟数
    const s = Math.floor(seconds % 60); // 计算秒数
    if (h > 0) {
        // 如果有小时，返回 "HH:MM:SS" 格式
        return `${h.toString().padStart(2, "0")}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
    }
    // 如果没有小时，返回 "MM:SS" 格式
    return `${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
}

// ===== 核心上传类 =====
// 分片上传器类，负责处理大文件的分片上传逻辑
export class ChunkUploader {
    private config: ChunkUploadConfig; // 分片上传配置
    private status: ChunkUploadStatus = "idle"; // 当前上传状态，默认为空闲
    private paused = false; // 是否暂停上传
    private aborted = false; // 是否取消上传
    private uploadId = ""; // 上传任务的唯一标识
    private totalChunks = 0; // 总分片数量
    private uploadedCount = 0; // 已上传的分片数量
    private startTime = 0; // 上传开始时间（用于计算速度）
    private file: File | null = null; // 待上传的文件对象
    private callbacks: UploadCallbacks | null = null; // 保存原始回调，供恢复时使用
    private fileMd5Value = ""; // 保存已计算的文件MD5，恢复时跳过哈希阶段

    // 构造函数，初始化上传器配置
    constructor(config?: Partial<ChunkUploadConfig>) {
        this.config = getChunkUploadConfig(config); // 获取最终配置（合并默认值和传入值）
    }

    /** 判断文件是否需要分片上传 */
    shouldUseChunkUpload(file: File): boolean {
        // const fileSizeMB = (file.size / 1024 / 1024).toFixed(2);
        // const thresholdMB = (this.config.largeFileThreshold / 1024 / 1024).toFixed(2);
        // console.log(`[ChunkUpload] 文件大小: ${fileSizeMB}MB, 分片阈值: ${thresholdMB}MB`);
        return file.size >= this.config.largeFileThreshold; // 文件大小达到阈值时使用分片上传
    }

    /** 获取当前状态 */
    getStatus(): ChunkUploadStatus {
        return this.status; // 返回当前上传状态
    }

    /** 上传文件 */
    async upload(file: File, callbacks: UploadCallbacks): Promise<void> {
        this.file = file; // 保存文件引用
        this.callbacks = callbacks; // 保存原始回调，供恢复时使用
        this.aborted = false; // 重置取消标志
        this.paused = false; // 重置暂停标志
        this.uploadedCount = 0; // 重置已上传计数
        this.fileMd5Value = ""; // 重置已保存的MD5

        try {
            // 阶段1：计算MD5
            this.status = "hashing"; // 设置状态为计算哈希中
            this.emitProgress(callbacks, 0); // 发送初始进度

            // 计算文件的 MD5 哈希值，并在计算过程中更新进度
            const fileMd5 = await this.calculateFileMd5(file, (hashingPercent) => {
                this.emitProgress(callbacks, hashingPercent); // 发送哈希计算进度
            });

            // MD5 计算完成后保存，恢复时可直接使用
            this.fileMd5Value = fileMd5;

            if (this.aborted) return; // 如果已取消，直接返回

            // 执行上传流程（初始化 → 上传分片 → 合并）
            await this.doUploadFlow(file, fileMd5, callbacks);
        } catch (error: any) { // 捕获错误
            if (this.aborted) return; // 如果已取消，直接返回
            if (this.paused) { // 如果是暂停导致的中断
                this.status = "paused"; // 设置状态为已暂停
                this.emitProgress(callbacks, 0); // 发送进度
                return; // 直接返回
            }
            this.status = "error"; // 设置状态为错误
            this.emitProgress(callbacks, 0); // 发送进度
            callbacks.onError(error instanceof Error ? error : new Error(error?.message || "上传失败")); // 调用错误回调
        }
    }

    /**
     * 执行上传流程：初始化 → 上传分片 → 合并
     * 被 upload() 和 resumeFromUpload() 共用
     */
    private async doUploadFlow(file: File, fileMd5: string, callbacks: UploadCallbacks): Promise<void> {
        // 阶段2：初始化上传
        this.totalChunks = Math.ceil(file.size / this.config.chunkSize); // 计算总分片数量
        const initParams: ChunkInitParams = { // 构建初始化参数
            fileMd5, // 文件 MD5
            fileName: file.name, // 文件名
            fileSize: file.size, // 文件大小
            chunkSize: this.config.chunkSize, // 分片大小
            totalChunks: this.totalChunks // 总分片数
        };

        const initRes = await chunkInitAPI(initParams); // 调用初始化接口
        const initData = (initRes as any).data || initRes; // 获取响应数据

        // 秒传检测
        if (initData.existFile) { // 如果服务器已存在该文件（秒传）
            this.status = "done"; // 设置状态为完成
            this.emitProgress(callbacks, 100); // 发送100%进度
            callbacks.onSuccess(initData.existFile); // 调用成功回调
            return; // 直接返回，无需上传
        }

        this.uploadId = initData.uploadId; // 保存上传任务ID
        const uploadedChunks: number[] = initData.uploadedChunks || []; // 获取已上传的分片列表
        this.uploadedCount = uploadedChunks.length; // 更新已上传计数

        if (this.aborted) return; // 如果已取消，直接返回

        // 阶段3：上传分片
        this.status = "uploading"; // 设置状态为上传中
        this.startTime = Date.now(); // 记录开始时间
        this.emitProgress(callbacks, 0); // 发送初始进度

        await this.uploadChunks(file, uploadedChunks, callbacks); // 并发上传所有分片

        if (this.aborted) return; // 如果已取消，直接返回
        if (this.paused) { // 如果已暂停
            this.status = "paused"; // 设置状态为已暂停
            this.emitProgress(callbacks, 0); // 发送进度
            return; // 直接返回
        }

        // 阶段4：合并分片
        this.status = "merging"; // 设置状态为合并中
        this.emitProgress(callbacks, 100); // 发送100%进度

        const mergeParams: ChunkMergeParams = { // 构建合并参数
            uploadId: this.uploadId, // 上传任务ID
            fileMd5, // 文件 MD5
            fileName: file.name, // 文件名
            fileSize: file.size, // 文件大小
            totalChunks: this.totalChunks // 总分片数
        };

        const mergeRes = await chunkMergeAPI(mergeParams); // 调用合并接口
        const mergeData = (mergeRes as any).data || mergeRes; // 获取响应数据

        this.status = "done"; // 设置状态为完成
        this.emitProgress(callbacks, 100); // 发送100%进度
        callbacks.onSuccess(mergeData); // 调用成功回调
    }

    /** 暂停上传 */
    pause(): void {
        if (this.status === "uploading" || this.status === "hashing") { // 只有在上传中或计算哈希中才能暂停
            this.paused = true; // 设置暂停标志
            this.status = "paused"; // 更新状态为已暂停
        }
    }

    /** 恢复上传 */
    resume(): void {
        if (this.status !== "paused" || !this.file || !this.callbacks) return; // 前置条件检查

        this.paused = false; // 清除暂停标志
        const callbacks = this.callbacks; // 使用保存的原始回调

        if (this.fileMd5Value) {
            // MD5 已计算完成，跳过哈希阶段直接续传
            this.resumeFromUpload(callbacks);
        } else {
            // MD5 未完成（哈希阶段暂停），需要重新开始完整流程
            this.upload(this.file, callbacks);
        }
    }

    /**
     * 从上传阶段恢复（跳过MD5计算）
     * 直接调用 ChunkInit 获取已上传分片列表，然后续传剩余分片
     */
    private async resumeFromUpload(callbacks: UploadCallbacks): Promise<void> {
        try {
            this.totalChunks = Math.ceil(this.file!.size / this.config.chunkSize);

            // 调用初始化接口获取已上传分片列表（断点续传）
            const initParams: ChunkInitParams = {
                fileMd5: this.fileMd5Value,
                fileName: this.file!.name,
                fileSize: this.file!.size,
                chunkSize: this.config.chunkSize,
                totalChunks: this.totalChunks
            };

            const initRes = await chunkInitAPI(initParams);
            const initData = (initRes as any).data || initRes;

            // 秒传检测
            if (initData.existFile) {
                this.status = "done";
                this.emitProgress(callbacks, 100);
                callbacks.onSuccess(initData.existFile);
                return;
            }

            this.uploadId = initData.uploadId;
            const uploadedChunks: number[] = initData.uploadedChunks || [];
            this.uploadedCount = uploadedChunks.length;

            if (this.aborted) return;

            // 上传剩余分片
            this.status = "uploading";
            this.startTime = Date.now();
            this.emitProgress(callbacks, 0);

            await this.uploadChunks(this.file!, uploadedChunks, callbacks);

            if (this.aborted) return;
            if (this.paused) {
                this.status = "paused";
                this.emitProgress(callbacks, 0);
                return;
            }

            // 合并分片
            this.status = "merging";
            this.emitProgress(callbacks, 100);

            const mergeParams: ChunkMergeParams = {
                uploadId: this.uploadId,
                fileMd5: this.fileMd5Value,
                fileName: this.file!.name,
                fileSize: this.file!.size,
                totalChunks: this.totalChunks
            };

            const mergeRes = await chunkMergeAPI(mergeParams);
            const mergeData = (mergeRes as any).data || mergeRes;

            this.status = "done";
            this.emitProgress(callbacks, 100);
            callbacks.onSuccess(mergeData);
        } catch (error: any) {
            if (this.aborted) return;
            if (this.paused) {
                this.status = "paused";
                this.emitProgress(callbacks, 0);
                return;
            }
            this.status = "error";
            this.emitProgress(callbacks, 0);
            callbacks.onError(error instanceof Error ? error : new Error(error?.message || "上传失败"));
        }
    }

    /** 取消上传 */
    async cancel(): Promise<void> {
        this.aborted = true; // 设置取消标志
        this.paused = false; // 清除暂停标志
        this.status = "idle"; // 重置状态为空闲
        this.fileMd5Value = ""; // 清空已保存的MD5
        this.callbacks = null; // 清空已保存的回调

        if (this.uploadId) { // 如果有上传任务ID
            try {
                await chunkCancelAPI({ uploadId: this.uploadId }); // 调用取消接口
            } catch (e) {
                // 取消失败不影响主流程（静默处理）
            }
            this.uploadId = ""; // 清空上传任务ID
        }
    }

    /** 计算文件MD5 */
    private calculateFileMd5(file: File, onProgress: (percent: number) => void): Promise<string> {
        return new Promise((resolve, reject) => { // 返回一个 Promise
            const chunkSize = this.config.chunkSize; // 获取分片大小
            const chunks = Math.ceil(file.size / chunkSize); // 计算总分片数
            let currentChunk = 0; // 当前分片索引
            const spark = new SparkMD5.ArrayBuffer(); // 创建 SparkMD5 实例
            const reader = new FileReader(); // 创建文件读取器

            // 定义加载下一个分片的函数
            const loadNext = () => {
                if (this.aborted || this.paused) { // 如果已取消或暂停
                        reject(new Error("PAUSED")); // 使用特定标记，便于 catch 中区分暂停和真正的错误
                        return; // 返回
                    }

                const start = currentChunk * chunkSize; // 计算分片起始位置
                const end = Math.min(start + chunkSize, file.size); // 计算分片结束位置
                reader.readAsArrayBuffer(file.slice(start, end)); // 读取分片数据
            };

            // 文件读取成功的回调
            reader.onload = (e) => {
                if (e.target?.result) { // 如果有读取结果
                    spark.append(e.target.result as ArrayBuffer); // 将数据添加到哈希计算器
                }
                currentChunk++; // 移动到下一个分片

                const hashingPercent = Math.round((currentChunk / chunks) * 100); // 计算哈希进度百分比
                onProgress(hashingPercent); // 调用进度回调

                if (currentChunk < chunks) { // 如果还有未处理的分片
                    // 使用 setTimeout 避免阻塞UI
                    setTimeout(loadNext, 0);
                } else { // 所有分片处理完毕
                    resolve(spark.end()); // 返回最终的 MD5 哈希值
                }
            };

            // 文件读取失败的回调
            reader.onerror = () => {
                reject(new Error("文件读取失败")); // 拒绝 Promise
            };

            loadNext(); // 开始加载第一个分片
        });
    }

    /** 并发上传分片 */
    private async uploadChunks(file: File, uploadedChunks: number[], callbacks: UploadCallbacks): Promise<void> {
        // 构建待上传分片队列（跳过已上传的）
        const pendingQueue: number[] = []; // 待上传队列
        for (let i = 1; i <= this.totalChunks; i++) { // 遍历所有分片
            if (!uploadedChunks.includes(i)) { // 如果分片未上传
                pendingQueue.push(i); // 添加到待上传队列
            }
        }

        if (pendingQueue.length === 0) return; // 如果没有待上传的分片，直接返回

        // 并发控制
        const concurrency = this.config.concurrency; // 获取并发数量
        let running = 0; // 当前运行中的上传任务数
        let resolveAll: () => void; // Promise 的 resolve 函数
        let rejectAll: (error: Error) => void; // Promise 的 reject 函数
        const allPromise = new Promise<void>((resolve, reject) => { // 创建一个 Promise 用于等待所有上传完成
            resolveAll = resolve; // 保存 resolve 函数
            rejectAll = reject; // 保存 reject 函数
        });

        // 尝试上传下一个分片的函数
        const tryUploadNext = () => {
            // 检查是否应该停止
            if (this.aborted) { // 如果已取消
                if (running === 0) resolveAll!(); // 如果没有运行中的任务，完成 Promise
                return; // 返回
            }
            if (this.paused) { // 如果已暂停
                if (running === 0) resolveAll!(); // 如果没有运行中的任务，完成 Promise
                return; // 返回
            }
            if (pendingQueue.length === 0) { // 如果队列为空
                if (running === 0) resolveAll!(); // 如果没有运行中的任务，完成 Promise
                return; // 返回
            }

            // 循环启动上传任务，直到达到并发上限
            while (running < concurrency && pendingQueue.length > 0 && !this.aborted && !this.paused) {
                const chunkIndex = pendingQueue.shift()!; // 从队列中取出一个分片
                running++; // 增加运行计数
                this.uploadSingleChunk(file, chunkIndex, callbacks) // 上传单个分片
                    .then(() => { // 上传成功
                        running--; // 减少运行计数
                        this.uploadedCount++; // 增加已上传计数
                        this.emitProgress(callbacks, 0); // 发送进度更新
                        tryUploadNext(); // 尝试上传下一个
                    })
                    .catch((error) => { // 上传失败
                        running--; // 减少运行计数
                        if (!this.aborted && !this.paused) { // 如果未取消且未暂停
                            rejectAll!(error); // 拒绝 Promise
                        } else { // 如果已取消或暂停
                            tryUploadNext(); // 继续尝试上传下一个
                        }
                    });
            }
        };

        tryUploadNext(); // 开始上传
        return allPromise; // 返回 Promise
    }

    /** 上传单个分片（含重试） */
    private async uploadSingleChunk(file: File, chunkIndex: number, _callbacks: UploadCallbacks): Promise<void> {
        const start = (chunkIndex - 1) * this.config.chunkSize; // 计算分片起始位置
        const end = Math.min(start + this.config.chunkSize, file.size); // 计算分片结束位置
        const chunkBlob = file.slice(start, end); // 切取分片数据

        let lastError: Error | null = null; // 保存最后一次错误

        // 重试循环
        for (let attempt = 1; attempt <= this.config.maxRetry; attempt++) {
            if (this.aborted || this.paused) return; // 如果已取消或暂停，直接返回

            try {
                const formData = new FormData(); // 创建表单数据
                formData.append("file", chunkBlob); // 添加分片文件
                formData.append("uploadId", this.uploadId); // 添加上传任务ID
                formData.append("chunkIndex", String(chunkIndex)); // 添加分片索引
                formData.append("totalChunks", String(this.totalChunks)); // 添加总分片数
                formData.append("fileMd5", this.fileMd5Value); // 添加文件MD5（用于后端断点续传查询）

                await chunkUploadAPI(formData); // 调用上传接口
                return; // 上传成功，直接返回
            } catch (error: any) { // 捕获错误
                lastError = error instanceof Error ? error : new Error(error?.message || "分片上传失败"); // 保存错误
                // 如果是取消或暂停导致的错误，不重试
                if (this.aborted || this.paused) return; // 直接返回
                // 重试前等待一小段时间
                if (attempt < this.config.maxRetry) { // 如果还有重试机会
                    await new Promise(r => setTimeout(r, 1000 * attempt)); // 等待一段时间（递增延迟）
                }
            }
        }

        // 所有重试都失败，抛出错误
        throw lastError || new Error(`分片 ${chunkIndex} 上传失败`); // 抛出最后一次错误
    }

    /** 发送进度回调 */
    private emitProgress(callbacks: UploadCallbacks, hashingPercent: number): void {
        const totalChunks = this.totalChunks || 1; // 获取总分片数（默认为1）
        const uploadedChunks = this.uploadedCount; // 获取已上传分片数

        let percent = 0; // 初始化进度百分比
        if (this.status === "hashing") { // 如果正在计算哈希
            percent = Math.round(hashingPercent * 0.1); // MD5计算占10%
        } else if (this.status === "uploading" || this.status === "paused") { // 如果正在上传或已暂停
            percent = 10 + Math.round((uploadedChunks / totalChunks) * 90); // 上传占90%
        } else if (this.status === "merging") { // 如果正在合并
            percent = 99; // 合并时显示99%
        } else if (this.status === "done") { // 如果已完成
            percent = 100; // 显示100%
        }

        // 计算速度和剩余时间
        let currentSpeed = ""; // 初始化当前速度
        let remainingTime = ""; // 初始化剩余时间
        if (this.status === "uploading" && this.startTime > 0 && uploadedChunks > 0) { // 如果正在上传且有开始时间
            const elapsed = (Date.now() - this.startTime) / 1000; // 计算已用时间（秒）
            const bytesUploaded = uploadedChunks * this.config.chunkSize; // 计算已上传字节数
            const speed = bytesUploaded / elapsed; // 计算上传速度（字节/秒）
            currentSpeed = formatSpeed(speed); // 格式化速度字符串

            const remainingBytes = (totalChunks - uploadedChunks) * this.config.chunkSize; // 计算剩余字节数
            const remaining = remainingBytes / speed; // 计算剩余时间（秒）
            remainingTime = formatRemainingTime(remaining); // 格式化剩余时间字符串
        }

        // 调用进度回调
        callbacks.onProgress({
            status: this.status, // 当前状态
            totalChunks, // 总分片数
            uploadedChunks, // 已上传分片数
            percent, // 进度百分比
            currentSpeed, // 当前速度
            remainingTime, // 剩余时间
            hashingPercent: this.status === "hashing" ? hashingPercent : (this.status === "uploading" || this.status === "done" ? 100 : 0) // 哈希进度百分比
        });
    }
}
