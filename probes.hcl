probe "redis_main" {
  wait = true
  redis {
    host {
      hostname = "localhost"
      port     = 6379
    }
    password = "supersecret"
  }
}

probe "app_http" {
  http {
    scheme = "https"
    host {
      hostname = "myapplication.com"
      port     = 443
    }
    path    = "/healthz"
    timeout = "10s"
  }
}

probe "local_mysql" {
  mysql {
    host {
      hostname = "127.0.0.1"
      port     = 3306
    }
    credentials {
      user     = "monitor"
      password = "password123"
    }
  }
}

probe "data_volume_check" {
  filesystem {
    path = "/srv/data"
  }
}

probe "rabbitmq_broker" {
    amqp {
        host {
            hostname = "rabbitmq.example.com"
            port = 5672
        }
        credentials {
            user = "guest"
            password = "guest"
        }
        virtual_host = "/"
    }
}

probe "default_smtp" {
    smtp {
        host {
            hostname = "smtp.example.com"
            port = 25
        }
    }
}

probe "app_mongodb" {
    mongodb {
        url = "mongodb://user:pass@localhost:27017/mydb"
    }
}
