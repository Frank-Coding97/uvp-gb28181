-- Fail closed: ownership metadata is required to safely reconcile live recordings.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Recorder ownership metadata must be retained; automatic schema rollback is disabled';
