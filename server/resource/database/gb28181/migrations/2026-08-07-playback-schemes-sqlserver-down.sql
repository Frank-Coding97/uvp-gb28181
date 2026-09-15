IF EXISTS (SELECT TOP 1 1 FROM [gb_playback_scheme_slot])
   OR EXISTS (SELECT TOP 1 1 FROM [gb_playback_scheme])
  THROW 51000, 'playback scheme tables are not empty', 1;
DROP TABLE IF EXISTS [gb_playback_scheme_slot];
DROP TABLE IF EXISTS [gb_playback_scheme];
