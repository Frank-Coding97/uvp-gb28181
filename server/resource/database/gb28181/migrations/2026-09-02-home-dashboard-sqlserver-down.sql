IF EXISTS (SELECT 1 FROM gb_dashboard_layout)
   OR EXISTS (SELECT 1 FROM gb_sip_metric_minute)
   OR EXISTS (SELECT 1 FROM gb_sip_metric_flush)
   OR EXISTS (SELECT 1 FROM gb_sip_metric_gap)
   OR EXISTS (SELECT 1 FROM gb_play_attempt)
  THROW 51000, 'home dashboard tables are not empty', 1;
IF OBJECT_ID('gb_play_attempt','U') IS NOT NULL DROP TABLE gb_play_attempt;
IF OBJECT_ID('gb_sip_metric_gap','U') IS NOT NULL DROP TABLE gb_sip_metric_gap;
IF OBJECT_ID('gb_sip_metric_flush','U') IS NOT NULL DROP TABLE gb_sip_metric_flush;
IF OBJECT_ID('gb_sip_metric_minute','U') IS NOT NULL DROP TABLE gb_sip_metric_minute;
IF OBJECT_ID('gb_dashboard_layout','U') IS NOT NULL DROP TABLE gb_dashboard_layout;
