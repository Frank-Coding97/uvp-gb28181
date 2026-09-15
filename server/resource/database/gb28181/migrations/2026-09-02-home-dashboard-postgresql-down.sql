DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM gb_dashboard_layout LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_sip_metric_minute LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_sip_metric_flush LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_sip_metric_gap LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_play_attempt LIMIT 1) THEN
    RAISE EXCEPTION 'home dashboard tables are not empty';
  END IF;
END $$;
DROP TABLE IF EXISTS gb_play_attempt;
DROP TABLE IF EXISTS gb_sip_metric_gap;
DROP TABLE IF EXISTS gb_sip_metric_flush;
DROP TABLE IF EXISTS gb_sip_metric_minute;
DROP TABLE IF EXISTS gb_dashboard_layout;
