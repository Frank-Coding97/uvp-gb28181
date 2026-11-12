-- Media node single-port RTP receive mode (see the MySQL variant for context).
ALTER TABLE meta_node
  ADD COLUMN IF NOT EXISTS rtp_receive_mode VARCHAR(8) NOT NULL DEFAULT 'multi',
  ADD COLUMN IF NOT EXISTS rtp_proxy_port BIGINT NOT NULL DEFAULT 10000;