# AsterFlow 管理端

React 18 + TypeScript + Ant Design + TanStack Query + Zustand + Vite。
当前接入系统状态、登录、管理员用户管理，以及空间列表/创建和成员管理；沿用此前管理端的页面、表单与布局，品牌和会话键独立。

从仓库根目录执行：

```bash
make web-install
make web-dev
```

默认http://127.0.0.1:15174，代理到Go后端http://127.0.0.1:18090。API调用保持相对路径，不需要开发期CORS。
其他端口/后端可用ASTERFLOW_WEB_PORT、ASTERFLOW_DEV_API_TARGET覆盖。Ctrl+C停止当前前端进程。

```bash
make web-check
```

认证：登录响应保存token/user/expires_at；业务请求附带Bearer，401清理会话并回登录页，403保留身份并显示权限错误。
前端菜单与成员按钮仅改善交互，后端Auth/RequireGlobalAdmin及IAM Service负责权限。退出清理登录态和查询缓存。
用户列表每页10条，向服务端取11条以判断下一页，不虚构total；下一页offset增加10。
密码表单按UTF-8字节数检查8至72字节，服务端bcrypt约束仍是最终依据。

当前空间：空间页选择时用显式X-Workspace-ID调用/context，验证成功才持久化workspaceId；刷新重查角色，403/404清除失效选择，失败切换保留原空间。普通空间业务请求自动携带选中空间，全局接口和按URL指定空间的成员管理接口保持独立。退出/新登录清除空间选择，角色不持久化。旧请求401不会清除之后创建的新token会话。
