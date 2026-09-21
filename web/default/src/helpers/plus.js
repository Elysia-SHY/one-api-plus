import { API } from './api';

// 统一封装 /api/plus/* 全部接口
// one-api 返回信封：{ success, message, data }，axios 拦截器只在网络/HTTP 错误时弹错，
// 业务层 success=false 需要页面自行判断。这里统一返回信封（r.data）。
const B = '/api/plus';

const get = (url, params) => API.get(url, { params }).then((r) => r.data);
const post = (url, body) => API.post(url, body).then((r) => r.data);
const put = (url, body) => API.put(url, body).then((r) => r.data);
const del = (url) => API.delete(url).then((r) => r.data);

const PlusAPI = {
  // 总览 / 同步
  getStatus: () => get(`${B}/status`),
  getCatalog: (params = {}) => get(`${B}/catalog`, params),
  updateCatalogModel: (body) => put(`${B}/catalog`, body),
  enableModel: (body) => post(`${B}/catalog/enable`, body),
  syncModels: (scope) => post(`${B}/sync${scope ? `?scope=${scope}` : ''}`),
  syncChannel: (id) => post(`${B}/sync/${id}`),
  fetchChannelModels: (id) => get(`${B}/catalog/fetch/${id}`),

  // 健康
  getHealthList: () => get(`${B}/health`),
  checkChannels: () => post(`${B}/health/check`),
  getChannelHealth: (id) => get(`${B}/health/channel/${id}`),

  // 别名
  getAliasList: () => get(`${B}/alias`),
  addAlias: (body) => post(`${B}/alias`, body),
  updateAlias: (body) => put(`${B}/alias`, body),
  deleteAlias: (id) => del(`${B}/alias/${id}`),

  // 定价
  getPriceList: () => get(`${B}/price`),
  upsertPrice: (body) => post(`${B}/price`, body),
  deletePrice: (id) => del(`${B}/price/${id}`),

  // 成本 / 预算
  getCostStat: (params = {}) => get(`${B}/cost`, params),
  predictCost: (params = {}) => get(`${B}/cost/predict`, params),
  getBudget: (params = {}) => get(`${B}/budget`, params),
  updateBudget: (body) => put(`${B}/budget`, body),

  // 仪表盘 / 负载 / 路由
  getDashboard: (params = {}) => get(`${B}/dashboard`, params),  getLoad: () => get(`${B}/load`),
  getRouting: () => get(`${B}/routing`),
  updateRouting: (body) => put(`${B}/routing`, body),

  // 限流
  getRateLimit: () => get(`${B}/ratelimit`),

  // 能力库
  getCapabilities: () => get(`${B}/capability`),
  searchCapabilities: (params = {}) => get(`${B}/capability/search`, params),
  getCapability: (name) => get(`${B}/capability/${encodeURIComponent(name)}`),
  refreshCapability: (name, force) =>
    post(`${B}/capability/${encodeURIComponent(name)}/refresh${force ? '?force=1' : ''}`),
  updateCapability: (body) => put(`${B}/capability`, body),

  // 模型组
  getModelGroups: () => get(`${B}/group/model`),
  addModelGroup: (body) => post(`${B}/group/model`, body),
  updateModelGroup: (body) => put(`${B}/group/model`, body),
  removeModelGroup: (id) => del(`${B}/group/model/${id}`),
  addGroupMember: (body) => post(`${B}/group/model/member`, body),
  removeGroupMember: (id) => del(`${B}/group/model/member/${id}`),
  autoModelGroups: (body = {}) => post(`${B}/group/model/auto`, body),

  // 当前用户可用的模型（含模型组逻辑名）
  getUserAvailableModels: () => API.get('/api/user/available_models').then((r) => r.data),

  // MCP
  getMCPServers: () => get(`${B}/mcp`),
  upsertMCPServer: (body) => post(`${B}/mcp`, body),
  removeMCPServer: (id) => del(`${B}/mcp/${id}`),
  discoverMCPTools: () => post(`${B}/mcp/discover`),
  callMCPTool: (body) => post(`${B}/mcp/call`, body),

  // 记忆
  getMemory: (params = {}) => get(`${B}/memory`, params),
  addMemory: (body) => post(`${B}/memory`, body),
  deleteMemory: (id) => del(`${B}/memory/${id}`),
  clearMemory: () => del(`${B}/memory`),
  searchMemory: (params = {}) => get(`${B}/memory/search`, params),

  // Responses
  getResponsesStatus: () => get(`${B}/responses`),
};

export default PlusAPI;
