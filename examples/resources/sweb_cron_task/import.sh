# Import an existing cron task by its raw crontab line (the API's task id):
# "<minute> <hour> <day> <month> <weekday> <command>".
terraform import sweb_cron_task.nightly_backup "30 3 1 12 7 /usr/bin/php /home/example/backup.php"
