-- Fail closed: directory and claim-version metadata are required to safely stop a job.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Work-recording directory metadata must be retained; automatic schema rollback is disabled';
