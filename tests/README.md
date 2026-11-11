# 自动测试

`testutil/` 保存框架测试的隔离夹具；其中证券数据生成、账号构造和权限参考算法只供 Go 测试使用，不由主服务或通用导入器引用。
`fixtures/` 保存独立预期和回归验收数据，`browser/` 保存浏览器验收脚本，`examples/` 验证 SQL/Python 数据示例及声明式导入。
`architecture/` 同时检查：生产源码不依赖测试或示例包，examples 中不允许出现 Go 源码。
用户使用 examples 不需要运行这些测试辅助代码。
