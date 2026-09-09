-- Fail closed: directory and claim-version metadata are required to safely stop a job.
THROW 51000, 'Work-recording directory metadata must be retained; automatic schema rollback is disabled', 1;
