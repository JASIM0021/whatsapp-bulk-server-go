module.exports = {
  apps: [
    {
      name: 'whatsapp-bulk-backend',
      script: './bin/server',
      cwd: '/root/project/whatsapp-bulk-server-go',
      instances: 1,
      exec_mode: 'fork',
      autorestart: true,
      watch: false,
      max_memory_restart: '500M',
      env_file: '.env',
      log_date_format: 'YYYY-MM-DD HH:mm:ss Z',
      merge_logs: true,
      min_uptime: '10s',
      max_restarts: 10,
      restart_delay: 4000,
    },
  ],
};
