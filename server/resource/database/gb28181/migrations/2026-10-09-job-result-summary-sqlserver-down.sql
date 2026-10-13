IF COL_LENGTH(N'dbo.sys_job_results', N'summary') IS NOT NULL
BEGIN
 ALTER TABLE [dbo].[sys_job_results] DROP COLUMN [summary];
END;
