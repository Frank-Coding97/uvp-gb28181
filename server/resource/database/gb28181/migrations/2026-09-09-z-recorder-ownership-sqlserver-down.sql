-- Fail closed: ownership metadata is required to safely reconcile live recordings.
THROW 51000, 'Recorder ownership metadata must be retained; automatic schema rollback is disabled', 1;
