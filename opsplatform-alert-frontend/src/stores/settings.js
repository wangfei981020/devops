import { defineStore } from 'pinia'
import { ref } from 'vue'
import api from '../api'
import { setDisplayTimezone, getDisplayTimezone } from '../utils/datetime'

// Platform-wide settings. The display timezone is the one every rendered
// timestamp depends on, so it is fetched once after sign-in and pushed into the
// datetime helpers rather than being read per component.
export const useSettingsStore = defineStore('settings', () => {
  const displayTimezone = ref('')
  const logRetentionDays = ref(0)
  const logRows = ref(0)
  const availableZones = ref([])
  const sample = ref('')
  const loaded = ref(false)

  async function load() {
    try {
      const res = await api.get('/settings')
      if (res.code === 0 && res.data) {
        apply(res.data)
      }
    } catch (e) {
      // A failed load leaves the browser's own zone in effect, which is a
      // reasonable guess and better than rendering nothing.
    } finally {
      loaded.value = true
    }
  }

  function apply(data) {
    displayTimezone.value = data.display_timezone || ''
    if (data.log_retention_days !== undefined) logRetentionDays.value = data.log_retention_days
    if (data.log_rows !== undefined) logRows.value = data.log_rows
    availableZones.value = data.available_zones || availableZones.value
    sample.value = data.sample || ''
    setDisplayTimezone(displayTimezone.value)
  }

  async function save({ zone, retentionDays }) {
    const res = await api.put('/settings', {
      display_timezone: zone,
      log_retention_days: retentionDays,
    })
    if (res.code === 0 && res.data) apply(res.data)
    return res
  }

  return {
    displayTimezone, logRetentionDays, logRows, availableZones, sample, loaded,
    load, apply, save, getDisplayTimezone,
  }
})
