# JS Plugin WebUI TODO

- [x] 支持 `js/upload` 上传 `.zip`
- [x] 约定 zip 结构：`1` 个 `.js/.ts` 插件入口 + `webui/` 目录
- [x] 解包路径：`data/<instance>/scripts/_<zip-filename>/`
- [x] 校验 `webui/index.html` 必须存在
- [x] JS 扫描时跳过 `webui` 目录，避免静态资源 `.js` 被误加载为插件
- [x] 删除 zip 插件时联动删除解包目录和原 zip 文件
- [x] 提供 WebUI 子路径路由：`/webui/:plugin/*`
- [x] 支持静态资源访问和 SPA fallback 到 `index.html`
- [x] `js/list` 返回 `webUIPath`
- [x] `js/get_configs` 返回 `webUIPath`
- [x] 前端上传 accept 增加 `.zip`
- [x] 前端插件列表/配置页增加 WebUI 跳转入口

## Notes

- 上传 zip 后，插件脚本与 `webui` 会落在同一解包目录。
- WebUI 通过海豹 UI 同域子路径访问：`/webui/<plugin>/`。
- 前端可直接使用 API 返回的 `webUIPath` 展示跳转链接。
