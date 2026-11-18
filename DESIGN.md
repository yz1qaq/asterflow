# AsterFlow管理端设计

现有视觉约定记录，不新增品牌方案。来源：web/admin/src/main.tsx、styles.css及已批准的前端复用方式。

- React18、Ant Design5、中文locale；顶部导航，浅蓝背景与白色工作区。
- 系统无衬线字体；主色#3f5fce，正文#24314d，次级文字#62718a。
- 复用page-heading、page-description、workspace-current与现有表格/Modal样式。
- 主操作使用蓝色按钮；状态同时提供文字与Tag颜色；错误提供重试。
- 模型页沿固定参考ModelsPage的账户/部署页签、创建、轮换和价格编辑流程，仅接已保存的API；Profile页签沿用相同表格与表单；环境绑定使用环境切换控件及同一套表单，绑定按工作空间和环境读取。
- 当前空间/登录变化清除表单，账户密钥不进入查询缓存、localStorage或成功提示。
- 保持现有桌面密度与键盘操作，不主动进行手机测试。
