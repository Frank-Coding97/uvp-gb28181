IF COL_LENGTH(N'dbo.sys_job_results', N'summary') IS NULL
BEGIN
 ALTER TABLE [dbo].[sys_job_results] ADD [summary] NVARCHAR(MAX) NULL;
END;
