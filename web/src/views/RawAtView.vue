<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import EmptyState from '../components/EmptyState.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  AT_PRESETS,
  applyATPreset,
  device,
  executeRawAT,
  parsedATResponse,
  rawATCommand,
  rawATPreset,
  rawATResponse,
} = useViewContext()
</script>

<template>
  <section>
    <Panel v-if="device.has('raw_at')" :eyebrow="t('rawAt.ready')" :title="t('rawAt.title')"
      ><a-form class="form" layout="vertical" @submit.prevent="executeRawAT"
        ><a-form-item :label="t('rawAt.preset')"
          ><a-select v-model:value="rawATPreset" @change="applyATPreset"
            ><a-select-option value="">{{ t('rawAt.selectPreset') }}</a-select-option
            ><a-select-option v-for="preset in AT_PRESETS" :key="preset.id" :value="preset.id"
              >{{ t(preset.labelKey) }} · {{ preset.command }}</a-select-option
            ></a-select
          ></a-form-item
        ><a-form-item :label="t('rawAt.command')"
          ><a-input
            v-model:value="rawATCommand"
            name="at-command"
            placeholder="AT+CSQ"
            @input="rawATPreset = ''" /></a-form-item
        ><a-button type="primary" html-type="submit" :disabled="!rawATCommand.trim()">{{
          t('common.execute')
        }}</a-button></a-form
      >
      <div v-if="parsedATResponse" class="at-result">
        <div class="at-result-heading">
          <h3>{{ t('rawAt.parsedTitle') }}</h3>
          <a-tag
            :color="
              parsedATResponse.statusKey.endsWith('error')
                ? 'error'
                : parsedATResponse.statusKey.endsWith('ok')
                  ? 'success'
                  : 'default'
            "
            >{{ t(parsedATResponse.statusKey) }}</a-tag
          >
        </div>
        <dl v-if="parsedATResponse.fields.length" class="at-fields">
          <template v-for="(field, index) in parsedATResponse.fields" :key="`${field.labelKey}-${index}`"
            ><dt>{{ t(field.labelKey) }}</dt>
            <dd>{{ field.valueKey ? t(field.valueKey) : field.value }}</dd></template
          >
        </dl>
        <EmptyState v-else :title="t('rawAt.noParsedFields')" />
      </div>
      <div v-if="rawATResponse" class="at-raw">
        <h3>{{ t('rawAt.rawTitle') }}</h3>
        <pre class="at-response">{{ rawATResponse }}</pre>
      </div></Panel
    ><Panel v-else :eyebrow="t('rawAt.unavailable')" :title="t('rawAt.title')"
      ><a-alert type="warning" show-icon :message="t('rawAt.unavailableDetail')"
    /></Panel>
  </section>
</template>
