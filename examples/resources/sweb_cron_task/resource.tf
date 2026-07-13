# Manage a shared-hosting crontab entry.
# Only numeric single-value schedule positions are expressible (no "*", ranges or
# steps — the API takes integers). Every field forces replacement.
resource "sweb_cron_task" "nightly_backup" {
  minute  = 30
  hour    = 3
  day     = 1
  month   = 12
  weekday = 7 # 0 and 7 both mean Sunday
  command = "/usr/bin/php /home/example/backup.php"
}
