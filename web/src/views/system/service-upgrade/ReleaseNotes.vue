<template>
  <div class="release-notes">
    <template v-if="blocks.length">
      <template v-for="(block, index) in blocks" :key="index">
        <p v-if="block.kind === 'heading'" class="release-notes-heading" :data-level="block.level">
          <ReleaseNoteRuns :runs="block.runs" />
        </p>
        <p v-else-if="block.kind === 'paragraph'" class="release-notes-paragraph">
          <ReleaseNoteRuns :runs="block.runs" />
        </p>
        <ul v-else class="release-notes-list">
          <li v-for="(item, itemIndex) in block.items" :key="itemIndex"><ReleaseNoteRuns :runs="item" /></li>
        </ul>
      </template>
    </template>
    <p v-else class="release-notes-empty">{{ emptyText }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import ReleaseNoteRuns from "./ReleaseNoteRuns.vue";
import { parseReleaseNotes } from "./releaseNotes";

const props = withDefaults(defineProps<{ text?: string | null; emptyText?: string }>(), {
  text: "",
  emptyText: "该版本未提供更新说明"
});

const blocks = computed(() => parseReleaseNotes(props.text));
</script>

<style scoped lang="scss">
.release-notes {
  line-height: 1.8;
  color: var(--color-text-2);
  overflow-wrap: anywhere;

  code {
    padding: 1px 5px;
    font-size: 0.92em;
    color: var(--color-text-1);
    background: var(--color-fill-2);
    border-radius: 4px;
  }

  strong {
    font-weight: 600;
    color: var(--color-text-1);
  }
}

.release-notes-heading {
  margin: 16px 0 8px;
  font-size: 14px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--color-text-1);

  &:first-child {
    margin-top: 0;
  }
}

.release-notes-paragraph {
  margin: 0 0 8px;

  &:last-child {
    margin-bottom: 0;
  }
}

.release-notes-list {
  padding-left: 20px;
  margin: 0 0 8px;
  list-style: disc outside;

  &:last-child {
    margin-bottom: 0;
  }

  li {
    margin-bottom: 6px;

    &:last-child {
      margin-bottom: 0;
    }
  }

  li::marker {
    color: var(--color-text-3);
  }
}

.release-notes-empty {
  margin: 0;
  color: var(--color-text-3);
}
</style>
