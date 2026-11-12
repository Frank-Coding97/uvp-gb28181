-- PostgreSQL rejects narrowing when any stored ParentID exceeds 20 characters.
ALTER TABLE "gb_channel" ALTER COLUMN "parent_id" TYPE VARCHAR(20);
