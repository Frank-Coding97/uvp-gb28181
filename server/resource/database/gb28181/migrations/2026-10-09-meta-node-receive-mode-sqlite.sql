-- Media node single-port RTP receive mode (see the MySQL variant for context).
-- Empty rtp_receive_mode means legacy multi-port; rtp_proxy_port is ZLM's
-- rtp_proxy.port used when the mode is 'single'.
ALTER TABLE "meta_node"
  ADD COLUMN "rtp_receive_mode" TEXT NOT NULL DEFAULT 'multi';
ALTER TABLE "meta_node"
  ADD COLUMN "rtp_proxy_port" INTEGER NOT NULL DEFAULT 10000;
