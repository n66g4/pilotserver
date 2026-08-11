# Cabana 静态资源（占位）

第一期 **不集成** 完整 Cabana 行程回放；管理端仅提供设备列表、SSH 与路线元数据查询。

## 后续计划（第二期）

1. 从开源 [Cabana](https://github.com/commaai/cabana) 构建静态产物（或拷贝已构建的 `dist/`）。
2. 将静态文件放入本目录（或子目录 `dist/`）。
3. 由 Nginx 挂载，例如：

   ```nginx
   location /cabana/ {
       alias /path/to/pilotserver/web/cabana/dist/;
   }
   ```

4. 配置 Cabana 读取 pilotserver 已上传的 route/segment 数据（路径与 admin API 对齐）。

当前目录仅作占位，便于部署文档引用；无需在本期编译或打包 Cabana。
