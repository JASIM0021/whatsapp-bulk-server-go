module.exports = {
  apps: [
    {
      name: 'whatsapp-bulk-backend',
      script: './server',
      cwd: '/home/ubuntu/whatsapp-bulk-server-go', // Update this path
      instances: 1,
      exec_mode: 'fork',
      autorestart: true,
      watch: false,
      max_memory_restart: '500M',
      env: {
        PORT: 4000,
        NODE_ENV: 'production',
        FRONTEND_URL: 'http://localhost:5174',
        WHATSAPP_SESSION_PATH: './whatsapp_session.db',
        UPLOAD_DIR: './uploads',
        MAX_FILE_SIZE: 10485760,
        RATE_LIMIT_MIN_DELAY: 3000,
        RATE_LIMIT_MAX_DELAY: 5000,
      },
      env_production: {
        PORT: 4000,
        NODE_ENV: 'production',
        FRONTEND_URL: 'https://your-frontend-domain.com',
        WHATSAPP_SESSION_PATH: '/var/lib/whatsapp/session.db',
        UPLOAD_DIR: '/var/lib/whatsapp/uploads',
        MAX_FILE_SIZE: 10485760,
        RATE_LIMIT_MIN_DELAY: 3000,
        RATE_LIMIT_MAX_DELAY: 5000,
      },
      // Use PM2 default logs location (~/.pm2/logs/)
      log_date_format: 'YYYY-MM-DD HH:mm:ss Z',
      merge_logs: true,
      min_uptime: '10s',
      max_restarts: 10,
      restart_delay: 4000,
    },
  ],
};
