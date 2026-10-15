n=int(input()); a=list(map(int,input().split())); cur=ans=a[0]
for x in a[1:]:
    cur=max(x,cur+x); ans=max(ans,cur)
print(ans)
