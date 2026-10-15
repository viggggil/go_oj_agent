from collections import deque
n,m=map(int,input().split()); a=[list(input().strip()) for _ in range(n)]; ans=0
for i in range(n):
    for j in range(m):
        if a[i][j]!='1': continue
        ans+=1; a[i][j]='0'; q=deque([(i,j)])
        while q:
            x,y=q.popleft()
            for dx,dy in ((1,0),(-1,0),(0,1),(0,-1)):
                u,v=x+dx,y+dy
                if 0<=u<n and 0<=v<m and a[u][v]=='1': a[u][v]='0'; q.append((u,v))
print(ans)
