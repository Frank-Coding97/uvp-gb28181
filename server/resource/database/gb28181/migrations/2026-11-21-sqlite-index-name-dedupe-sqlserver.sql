-- SQL Server 的索引名按表各自唯一（sys.indexes 的主键就是 object_id + name），
-- 重名索引天然可以并存，无冲突。
SELECT 1;
