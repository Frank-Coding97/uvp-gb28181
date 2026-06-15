import { ref, computed } from 'vue';
import type { SysJobResultsData, SysJobResultsListParams, SysJobResultsListResult } from '../api/sysjobresults';
import {
    getSysJobResultsList,
    deleteSysJobResults
} from '../api/sysjobresults';

export const useSysJobResultsPluginHook = () => {
    // State
    const dataList = ref<SysJobResultsData[]>([]);
    const loading = ref<boolean>(false);
    const total = ref<number>(0);
    const currentPage = ref<number>(1);
    const pageSize = ref<number>(10);
    const searchParams = ref<Partial<SysJobResultsListParams>>({});

    // Computed
    const getDataList = computed(() => dataList.value);
    const isLoading = computed(() => loading.value);
    const getTotal = computed(() => total.value);
    const getCurrentPage = computed(() => currentPage.value);
    const getPageSize = computed(() => pageSize.value);
    const getSearchParams = computed(() => searchParams.value);

    // Actions
    const fetchDataList = async (params?: Partial<SysJobResultsListParams>) => {
        loading.value = true;
        try {
            // 更新分页参数和搜索条件
            if (params?.pageNum !== undefined) {
                currentPage.value = params.pageNum;
            }
            if (params?.pageSize !== undefined) {
                pageSize.value = params.pageSize;
            }
            
            // 更新搜索参数
            if (params) {
                searchParams.value = { ...searchParams.value, ...params };
            }

            // 构造请求参数
            const requestParams: SysJobResultsListParams = {
                pageNum: currentPage.value,
                pageSize: pageSize.value,
                ...searchParams.value
            };

            const response: SysJobResultsListResult = await getSysJobResultsList(requestParams);

            // 根据返回的数据结构处理
            if (Array.isArray(response.data.list)) {
                // 如果返回的是数组格式（旧格式）
                dataList.value = response.data.list || [];
                total.value = response.data.total || 0;
            }
        } finally {
            loading.value = false;
        }
    };

    const deleteData = async (id: number) => {
        try {
            await deleteSysJobResults(id);
            dataList.value = dataList.value.filter((item: SysJobResultsData) => item.id !== id);
            // 减少总数
            total.value = Math.max(0, total.value - 1);
        } catch (error) {
            throw error;
        }
    };

    // 重置搜索条件
    const resetSearchParams = () => {
        searchParams.value = {};
        currentPage.value = 1;
    };

    return {
        // State
        dataList,
        loading,
        total,
        currentPage,
        pageSize,
        searchParams,

        // Computed
        getDataList,
        isLoading,
        getTotal,
        getCurrentPage,
        getPageSize,
        getSearchParams,

        // Actions
        fetchDataList,
        deleteData,
        resetSearchParams
    };
};