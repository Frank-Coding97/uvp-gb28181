-- API 分组整治（一次性数据修正 + 编码修复）
-- 生成源: tmp/apigroup/normalize.py —— 逐行按 id 收敛，幂等可重跑

UPDATE sys_api SET api_group = '仪表盘' WHERE id IN (465,494,495,496,497,498,529,535);
UPDATE sys_api SET api_group = '设备管理' WHERE id IN (238,239,240,241,242,243,334,335,344,345,346,347,348,349,350,456,457,458,459,460,464,466,468,469,470,471,474,482,485,486,487,488,489,490,491,492,493,500,501,503,504,505,518,521,522,523,524,525,536,588);
UPDATE sys_api SET api_group = '多屏播放' WHERE id IN (254,255,256,257,258,259,360,361,362,363,364,461,462,463,472,473,475,476,477,478,479,480,481,483,484,499,502,507,508,509,510,511,512,513,514,515,516,517,519,520,534,579,580,581,582,585,586,590,591);
UPDATE sys_api SET api_group = '告警管理' WHERE id IN (245,246,247,248,506);
UPDATE sys_api SET api_group = '图像库' WHERE id IN (587,589);
UPDATE sys_api SET api_group = '云端录像' WHERE id IN (310,311,312,313,314,315,316,336,337,338,374,375,377);
UPDATE sys_api SET api_group = '录像计划' WHERE id IN (378,379,380,381,382,383,384,385,386,387,388,389,390);
UPDATE sys_api SET api_group = '国标级联' WHERE id IN (317,318,319,320,321,322,323,324,326,327);
UPDATE sys_api SET api_group = 'SIP 接入信息' WHERE id IN (217,218,219,220,221,237);
UPDATE sys_api SET api_group = '国标服务配置' WHERE id IN (49,252,253,261,262,263,264,265,266,267,268,269,270,271,272,273,274,275,276,299,300,301,302,306,307,308,309,332,333,530,531,532);
UPDATE sys_api SET api_group = '国标接入安全' WHERE id IN (277,278,279,280,281,282,283,284,292,293,294,295);
UPDATE sys_api SET api_group = '设备权限工作台' WHERE id IN (367,368,369,370,371,372,373);
UPDATE sys_api SET api_group = 'OpenAPI 客户端' WHERE id IN (548,549,550,551,552,553,554,555,556,557,558);
UPDATE sys_api SET api_group = '流媒体管理' WHERE id IN (393,394,395,396,397,398,399,400,401,402,403,404,405,406,407,408,409,410,411,412,413,414,415,416,417,418,419,420,421,422,423,424,425,426,427,428,429,430,431,432,433,434,435,436,437,438,439,440,441,442,443,444,445,446,447,448,449,450,527,583);
UPDATE sys_api SET api_group = '流媒体管理' WHERE id IN (584);
UPDATE sys_api SET api_group = '日志中心' WHERE id IN (70,72,73,212,222,223,224,225,226,227,228,229,355,356,357,358,359,547);
UPDATE sys_api SET api_group = '用户管理' WHERE id IN (7,8,9,10,11,202);
UPDATE sys_api SET api_group = '角色管理' WHERE id IN (19,20,21,22,23,24,25,26,61);
UPDATE sys_api SET api_group = '菜单管理' WHERE id IN (12,13,14,15,16,17,35,36,74,75,197);
UPDATE sys_api SET api_group = '部门管理' WHERE id IN (18,37,38,39,40);
UPDATE sys_api SET api_group = '字典管理' WHERE id IN (27,28,41,42,43,44,45,46,47,48,50,51,52);
UPDATE sys_api SET api_group = '个人中心' WHERE id IN (6,53,54,89);
UPDATE sys_api SET api_group = '在线用户' WHERE id IN (351,352);
UPDATE sys_api SET api_group = '接口管理' WHERE id IN (29,30,31,32,33);
UPDATE sys_api SET api_group = '文件管理' WHERE id IN (55,56,57,58,59,60,213,214,215,216);
UPDATE sys_api SET api_group = '系统配置' WHERE id IN (62,63,64,467,528,533);
UPDATE sys_api SET api_group = '代码生成' WHERE id IN (105,106,187,188,189,190,191,192,193,194,195,196);
UPDATE sys_api SET api_group = '定时任务' WHERE id IN (203,204,205,206,207,208,209,210,211);
UPDATE sys_api SET api_group = '插件管理' WHERE id IN (198,199,200,201);
UPDATE sys_api SET api_group = '插件示例' WHERE id IN (65,66,67,68,69);
UPDATE sys_api SET api_group = '认证管理' WHERE id IN (1,2,3,4,5);

-- 编码损坏行的标题修复
UPDATE sys_api SET title = '读取视频参数' WHERE id = 580;
UPDATE sys_api SET title = '下发视频参数' WHERE id = 581;
